// Package apeye HTTP server for interacting with days-gone-by
package apeye

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/squeal"
	"github.com/google/uuid"
)

// API really its apeye but I kind of hate typing it like that.
type API struct {
	store     *blobby.Blobby
	squeal    *squeal.Squeal
	maxUpload int
	mu        sync.RWMutex // ;; uploads hold RLock, a purge holds the write lock
}

func NewAPI(store *blobby.Blobby, squeal *squeal.Squeal, maxUpload int) *API {
	return &API{
		store:     store,
		squeal:    squeal,
		maxUpload: maxUpload,
	}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()

	// POST
	mux.HandleFunc("POST /images", a.postImage)
	// GET
	mux.HandleFunc("GET /blobs/{sha256}", a.getBlob)
	mux.HandleFunc("GET /images/{id}", a.getImage)
	// DELETE
	mux.HandleFunc("DELETE /images/{id}", a.deleteImage)
	mux.HandleFunc("DELETE /blobs/{sha256}", a.deleteBlob)

	return mux
}

// TODO: move all these to their own files

var allowedImages = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

func (a *API) postImage(w http.ResponseWriter, r *http.Request) {
	// read up until our maxUpload
	r.Body = http.MaxBytesReader(w, r.Body, int64(a.maxUpload))
	body := bufio.NewReader(r.Body)
	// verify file format
	head, err := body.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
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
			logger.ErrorF(err, "POST /images failed file too large")
			http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
		default:
			// internal error
			logger.ErrorF(err, "POST /images failed error during Blobby.Put()")
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	id, err := a.squeal.AddImage(r.Context(), info.SHA256, info.Size, mime, r.URL.Query().Get("filename"))
	if err != nil {
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
	logger.InfoF("POST /images succeeded sha256 [%s]", info.SHA256)
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
		logger.ErrorF(err, "GET /blobs/%s internal error during Squeal.BlobMime", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	f, err := a.store.Open(sum)
	if err != nil {
		if errors.Is(err, blobby.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "GET /blobs/%s internal error during Blobby.Open", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mime)
	w.Header().Set("ETag", `"`+sum+`"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	logger.InfoF("GET /blobs/%s serving blob...", sum)
	http.ServeContent(w, r, "", time.Time{}, f)
}

// getImage ;;
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
		logger.ErrorF(err, "GET /images/%s internal error", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	logger.InfoF("GET /images/%s serving image...", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(img)
}

// purge ;; removes the bytes of the given blobs unless a live image still uses them.
// returns false if uploads are in flight, nothing is purged then and the bytes wait for a later purge
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

// deleteImage ;; tombstones an image then frees its blob if nothing else uses it
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
		logger.ErrorF(err, "DELETE /images/%s internal error during Squeal.DeleteImage", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// best effort, the image is already gone so a failed purge only delays freeing the bytes
	// only this images blob is looked at, other blobs are not our business
	if _, err := a.purge(r.Context(), []string{sha}); err != nil {
		logger.ErrorF(err, "DELETE /images/%s purge failed", id)
	}

	logger.InfoF("DELETE /images/%s succeeded", id)
	w.WriteHeader(http.StatusNoContent)
}

// deleteBlob ;; manually removes a blobs bytes, refuses if a live image still uses it
func (a *API) deleteBlob(w http.ResponseWriter, r *http.Request) {
	sum := r.PathValue("sha256")

	if !a.mu.TryLock() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "busy, try again", http.StatusServiceUnavailable)
		return
	}
	defer a.mu.Unlock()

	// BlobMime doubles as the does this blob exist check, it skips purged blobs
	if _, err := a.squeal.BlobMime(r.Context(), sum); err != nil {
		if errors.Is(err, squeal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		logger.ErrorF(err, "DELETE /blobs/%s internal error during Squeal.BlobMime", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	inUse, err := a.squeal.BlobInUse(r.Context(), sum)
	if err != nil {
		logger.ErrorF(err, "DELETE /blobs/%s internal error during Squeal.BlobInUse", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inUse {
		http.Error(w, "blob is used by a live image", http.StatusConflict)
		return
	}

	if err := a.store.Delete(sum); err != nil && !errors.Is(err, blobby.ErrNotFound) {
		logger.ErrorF(err, "DELETE /blobs/%s internal error during Blobby.Delete", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := a.squeal.MarkPurged(r.Context(), sum); err != nil {
		logger.ErrorF(err, "DELETE /blobs/%s internal error during Squeal.MarkPurged", sum)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	logger.InfoF("DELETE /blobs/%s succeeded", sum)
	w.WriteHeader(http.StatusNoContent)
}
