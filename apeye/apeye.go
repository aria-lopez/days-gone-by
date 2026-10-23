// Package apeye HTTP server for interacting with days-gone-by
package apeye

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/bouncer"
	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/squeal"
	"github.com/google/uuid"
)

// API really its apeye but I kind of hate typing it like that.
type API struct {
	store     *blobby.Blobby
	squeal    *squeal.Squeal
	bouncer   *bouncer.Bouncer
	maxUpload int
	mu        sync.RWMutex // ;; uploads hold RLock, a purge holds the write lock
}

func NewAPI(store *blobby.Blobby, squeal *squeal.Squeal, bouncer *bouncer.Bouncer, maxUpload int) *API {
	return &API{
		store:     store,
		squeal:    squeal,
		bouncer:   bouncer,
		maxUpload: maxUpload,
	}
}

// Routes ;; routes w/ a bouncer bouncer.Role[Viewer|Member|Admin] determines access level and is upstream Viewer->Admin
func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	b := a.bouncer

	// POST
	mux.Handle("POST /api/v1/images", b.Require(bouncer.RoleMember, a.postImage))
	// GET
	mux.Handle("GET /api/v1/blobs/{sha256}", b.Require(bouncer.RoleViewer, a.getBlob))
	mux.Handle("GET /api/v1/images/{id}", b.Require(bouncer.RoleViewer, a.getImage))
	// DELETE
	mux.Handle("DELETE /api/v1/images/{id}", b.Require(bouncer.RoleMember, a.deleteImage))
	mux.Handle("DELETE /api/v1/blobs/{sha256}", b.Require(bouncer.RoleAdmin, a.deleteBlob))

	return mux
}

// TODO: move all these to their own files

var allowedImages = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

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

func (a *API) postImage(w http.ResponseWriter, r *http.Request) {
	// read up until our maxUpload
	r.Body = http.MaxBytesReader(w, r.Body, int64(a.maxUpload))
	// and dont wait on clients that go quiet, a stuck upload would hold mu.RLock for ever
	body := bufio.NewReader(&deadlineReader{
		r:    r.Body,
		rc:   http.NewResponseController(w),
		idle: uploadIdle,
		hard: time.Now().Add(uploadMax),
	})
	// verify file format
	head, err := body.Peek(512)
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
	mime := http.DetectContentType(head)
	if !allowedImages[mime] {
		http.Error(w, "unsupported file type", http.StatusUnsupportedMediaType)
		return
	}

	// uploads can run together, but never while a purge is deciding what to delete
	a.mu.RLock()
	defer a.mu.RUnlock()

	// put that file into blobby :)
	info, err := a.store.Put(body)
	if err != nil {
		var itsTooBig *http.MaxBytesError
		switch {
		case errors.As(err, &itsTooBig):
			logger.ErrorF(err, "POST /api/v1/images failed file too large")
			http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, os.ErrDeadlineExceeded):
			logger.ErrorF(err, "POST /api/v1/images failed client stopped sending")
			http.Error(w, "upload timed out", http.StatusRequestTimeout)
		default:
			// internal error
			logger.ErrorF(err, "POST /api/v1/images failed error during Blobby.Put()")
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	who, _ := bouncer.From(r.Context())
	id, err := a.squeal.AddImage(r.Context(), info.SHA256, info.Size, mime, cleanFilename(r.URL.Query().Get("filename")), who.KeyID)
	if err != nil {
		logger.ErrorF(err, "POST /api/v1/images internal error during Squeal.AddImage")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"sha256": info.SHA256,
		"size":   info.Size,
		"mime":   mime,
		"id":     id,
	})
	logger.InfoF("POST /api/v1/images succeeded sha256 [%s] by key [%s]", info.SHA256, who.Name)
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

	logger.InfoF("GET /api/v1/blobs/%s serving blob...", sum)
	http.ServeContent(w, r, "", time.Time{}, f)
}

// getImage ;; gets an image
func (a *API) getImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		http.NotFound(w, r)
		return
	}

	img, err := a.squeal.GetImage(r.Context(), id)
	if err != nil {
		if errors.Is(err, squeal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "GET /api/v1/images/%s internal error", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	logger.InfoF("GET /api/v1/images/%s serving image...", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(img)
}

// purge ;; removes []shas ([]Blobs) unless a live record still uses them. returns false if uploads are in-flight
func (a *API) purge(ctx context.Context, shas []string) (bool, error) {
	if !a.mu.TryLock() {
		return false, nil
	}
	defer a.mu.Unlock()

	for _, sha := range shas {
		// checked again under the lock, an upload may have given it a live image since
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

// deleteImage ;; tombstone an image - not a hard delete
func (a *API) deleteImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		http.NotFound(w, r)
		return
	}

	sha, err := a.squeal.DeleteImage(r.Context(), id)
	if err != nil {
		if errors.Is(err, squeal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "DELETE /api/v1/images/%s internal error during Squeal.DeleteImage", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// take a look at if it needs to be purged
	if _, err := a.purge(r.Context(), []string{sha}); err != nil {
		logger.ErrorF(err, "DELETE /api/v1/images/%s purge failed", id)
	}

	logger.InfoF("DELETE /api/v1/images/%s succeeded", id)
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
		http.Error(w, "blob is used by a live image", http.StatusConflict)
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
