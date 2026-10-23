// Package apeye HTTP server for interacting with days-gone-by
package apeye

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/bouncer"
	"github.com/aria-lopez/days-gone-by/config"
	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/sniffer"
	"github.com/aria-lopez/days-gone-by/squeal"
	"github.com/google/uuid"
)

// API really its apeye but I kind of hate typing it like that.
type API struct {
	store   *blobby.Blobby
	squeal  *squeal.Squeal
	bouncer *bouncer.Bouncer
	cfg     config.Config
	mu      sync.RWMutex // ;; uploads hold RLock, a purge holds the write lock
}

func NewAPI(store *blobby.Blobby, squeal *squeal.Squeal, bouncer *bouncer.Bouncer, cfg config.Config) *API {
	return &API{
		store:   store,
		squeal:  squeal,
		bouncer: bouncer,
		cfg:     cfg,
	}
}

// Routes ;; routes w/ a bouncer bouncer.Role[Viewer|Member|Admin] determines access level and is upstream Viewer->Admin
func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	b := a.bouncer

	// POST
	mux.Handle("POST /api/v1/files", b.Require(bouncer.RoleMember, a.postFile))
	// GET
	mux.Handle("GET /api/v1/blobs/{sha256}", b.Require(bouncer.RoleViewer, a.getBlob))
	mux.Handle("GET /api/v1/files", b.Require(bouncer.RoleViewer, a.listFiles))
	mux.Handle("GET /api/v1/files/{id}", b.Require(bouncer.RoleViewer, a.getFile))
	// DELETE
	mux.Handle("DELETE /api/v1/files/{id}", b.Require(bouncer.RoleMember, a.deleteFile))
	mux.Handle("DELETE /api/v1/blobs/{sha256}", b.Require(bouncer.RoleAdmin, a.deleteBlob))

	return mux
}

// TODO: move all these to their own files

const maxFilename = 255

// cleanFilename ;; keep to 255 bytes
func cleanFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1 // drop it
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if len(s) > maxFilename {
		s = s[:maxFilename]
		// the cut may have landed in the middle of a character, step back until its whole again
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func (a *API) postFile(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	top := a.cfg.Largest()
	if r.ContentLength > int64(top.MaxSize) {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// until we know the kind the largest limits apply. dont wait on clients that go quiet, a stuck upload would hold mu.RLock for ever
	dr := &deadlineReader{
		r:    r.Body,
		rc:   http.NewResponseController(w),
		idle: a.cfg.IdleTimeout.Std(),
		hard: start.Add(top.Timeout.Std()),
	}
	body := bufio.NewReader(dr)
	head, err := body.Peek(sniffer.HeadSize)
	if err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// connected, sent headers, then never sent a body
			http.Error(w, "upload timed out", http.StatusRequestTimeout)
			return
		}
		http.Error(w, "couldn't read upload", http.StatusBadRequest)
		return
	}
	if len(head) == 0 {
		http.Error(w, "empty upload", http.StatusBadRequest)
		return
	}
	mime, kind, ok := sniffer.Sniff(head)
	if !ok {
		http.Error(w, "unsupported file type "+mime, http.StatusUnsupportedMediaType)
		return
	}

	limit := a.cfg.Limit(kind)
	tooBig := fmt.Sprintf("%s files can be at most %s", kind, limit.MaxSize)
	if r.ContentLength > int64(limit.MaxSize) {
		http.Error(w, tooBig, http.StatusRequestEntityTooLarge)
		return
	}
	dr.hard = start.Add(limit.Timeout.Std())
	upload := http.MaxBytesReader(w, io.NopCloser(body), int64(limit.MaxSize))

	// uploads can run together, but never while a purge is deciding what to delete
	a.mu.RLock()
	defer a.mu.RUnlock()

	// put that file into blobby :)
	info, err := a.store.Put(upload)
	if err != nil {
		var itsTooBig *http.MaxBytesError
		switch {
		case errors.As(err, &itsTooBig):
			logger.ErrorF(err, "POST /api/v1/files failed file too large")
			http.Error(w, tooBig, http.StatusRequestEntityTooLarge)
		case errors.Is(err, os.ErrDeadlineExceeded):
			logger.ErrorF(err, "POST /api/v1/files failed client stopped sending")
			http.Error(w, "upload timed out", http.StatusRequestTimeout)
		default:
			// internal error
			logger.ErrorF(err, "POST /api/v1/files failed error during Blobby.Put()")
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	who, _ := bouncer.From(r.Context())
	id, err := a.squeal.AddFile(r.Context(), info.SHA256, info.Size, mime, cleanFilename(r.URL.Query().Get("filename")), who.KeyID)
	if err != nil {
		logger.ErrorF(err, "POST /api/v1/files internal error during Squeal.AddFile")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"sha256": info.SHA256,
		"size":   info.Size,
		"mime":   mime,
		"kind":   kind,
		"id":     id,
	})
	logger.InfoF("POST /api/v1/files succeeded sha256 [%s] by key [%s]", info.SHA256, who.Name)
}

// getBlob ;; returns [Blob] of a given sha256 if it exists
func (a *API) getBlob(w http.ResponseWriter, r *http.Request) {
	sum := r.PathValue("sha256")

	mime, err := a.squeal.BlobMime(r.Context(), sum)
	if errors.Is(err, squeal.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		logger.ErrorF(err, "GET /api/v1/blobs/%s internal error during Squeal.BlobMime", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	f, err := a.store.Open(sum)
	if err != nil {
		if errors.Is(err, blobby.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "GET /api/v1/blobs/%s internal error during Blobby.Open", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mime)
	w.Header().Set("ETag", `"`+sum+`"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !sniffer.Inline(mime) {
		w.Header().Set("Content-Disposition", "attachment")
	}

	logger.InfoF("GET /api/v1/blobs/%s serving blob...", sum)
	http.ServeContent(w, r, "", time.Time{}, f)
}

// getFile ;; gets a file
func (a *API) getFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		http.NotFound(w, r)
		return
	}

	file, err := a.squeal.GetFile(r.Context(), id)
	if err != nil {
		if errors.Is(err, squeal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "GET /api/v1/files/%s internal error", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	logger.InfoF("GET /api/v1/files/%s serving file...", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(file)
}

const (
	defaultPage = 50
	maxPage     = 200
)

// listFiles ;; one page of files, newest first. "next" is the id to pass as ?after= for the following page, null on the last one
func (a *API) listFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := defaultPage
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPage {
			http.Error(w, fmt.Sprintf("limit must be 1 to %d", maxPage), http.StatusBadRequest)
			return
		}
		limit = n
	}
	after := q.Get("after")
	if u, err := uuid.Parse(after); after != "" && (err != nil || u.String() != after) {
		http.Error(w, "after must be a file id", http.StatusBadRequest)
		return
	}
	sha := strings.ToLower(q.Get("sha256"))
	if _, err := hex.DecodeString(sha); sha != "" && (err != nil || len(sha) != 64) {
		http.Error(w, "sha256 must be 64 hex characters", http.StatusBadRequest)
		return
	}

	// one more than asked for tells us whether there is a next page
	files, err := a.squeal.ListFiles(r.Context(), after, sha, limit+1)
	if err != nil {
		logger.ErrorF(err, "GET /api/v1/files internal error during Squeal.ListFiles")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var next *string
	if len(files) > limit {
		files = files[:limit]
		next = &files[limit-1].ID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"files": files, "next": next})
}

// purge ;; removes []shas ([]Blobs) unless a live record still uses them. returns false if uploads are in-flight
func (a *API) purge(ctx context.Context, shas []string) (bool, error) {
	if !a.mu.TryLock() {
		return false, nil
	}
	defer a.mu.Unlock()

	for _, sha := range shas {
		// checked again under the lock, an upload may have given it a live file since
		inUse, err := a.squeal.BlobInUse(ctx, sha)
		if err != nil {
			return true, err
		}
		if inUse {
			continue
		}
		// file first then the db, if we crash in between the next purge just marks it
		if err := a.store.Delete(sha); err != nil && !errors.Is(err, blobby.ErrNotFound) {
			return true, err
		}
		if err := a.squeal.MarkPurged(ctx, sha); err != nil {
			return true, err
		}
		logger.InfoF("purged blob sha256 [%s]", sha)
	}
	return true, nil
}

// deleteFile ;; tombstone a file - not a hard delete
func (a *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		http.NotFound(w, r)
		return
	}

	sha, err := a.squeal.DeleteFile(r.Context(), id)
	if err != nil {
		if errors.Is(err, squeal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "DELETE /api/v1/files/%s internal error during Squeal.DeleteFile", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// take a look at if it needs to be purged
	if _, err := a.purge(r.Context(), []string{sha}); err != nil {
		logger.ErrorF(err, "DELETE /api/v1/files/%s purge failed", id)
	}

	logger.InfoF("DELETE /api/v1/files/%s succeeded", id)
	w.WriteHeader(http.StatusNoContent)
}

// deleteBlob ;; manually removes a blob, refuses if a live record still uses it
func (a *API) deleteBlob(w http.ResponseWriter, r *http.Request) {
	sum := r.PathValue("sha256")

	if !a.mu.TryLock() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "busy, try again", http.StatusServiceUnavailable)
		return
	}
	defer a.mu.Unlock()

	// check if it exists
	if _, err := a.squeal.BlobMime(r.Context(), sum); err != nil {
		if errors.Is(err, squeal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "DELETE /api/v1/blobs/%s internal error during Squeal.BlobMime", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	inUse, err := a.squeal.BlobInUse(r.Context(), sum)
	if err != nil {
		logger.ErrorF(err, "DELETE /api/v1/blobs/%s internal error during Squeal.BlobInUse", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inUse {
		http.Error(w, "blob is used by a live file", http.StatusConflict)
		return
	}

	if err := a.store.Delete(sum); err != nil && !errors.Is(err, blobby.ErrNotFound) {
		logger.ErrorF(err, "DELETE /api/v1/blobs/%s internal error during Blobby.Delete", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := a.squeal.MarkPurged(r.Context(), sum); err != nil {
		logger.ErrorF(err, "DELETE /api/v1/blobs/%s internal error during Squeal.MarkPurged", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	logger.InfoF("DELETE /api/v1/blobs/%s succeeded", sum)
	w.WriteHeader(http.StatusNoContent)
}
