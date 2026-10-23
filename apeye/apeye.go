// Package apeye HTTP server for interacting with days-gone-by
package apeye

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/logger"
)

// API really its apeye but I kind of hate typing it like that.
type API struct {
	store     *blobby.Blobby
	maxUpload int
}

func NewAPI(store *blobby.Blobby, maxUpload int) *API {
	return &API{
		store, maxUpload,
	}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()

	// POST
	mux.HandleFunc("POST /images", a.postImage)

	return mux
}

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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"sha256": info.SHA256,
		"size":   info.Size,
		"mime":   mime,
	})
	logger.InfoF("POST /images succeeded sha256 [%s]", info.SHA256)
}
