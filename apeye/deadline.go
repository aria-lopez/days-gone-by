package apeye

import (
	"errors"
	"io"
	"net/http"
	"time"
)

// FIXME: [config]
const (
	uploadIdle = 30 * time.Second // ;; a client that sends nothing for this long gets cut off
	uploadMax  = 30 * time.Minute // ;; no upload may take longer than this, however steady
)

// deadlineReader ;; allows a slow read to push forward, but only until the hard cutoff time.
type deadlineReader struct {
	r    io.Reader
	rc   *http.ResponseController
	idle time.Duration
	hard time.Time
}

func (d *deadlineReader) Read(p []byte) (int, error) {
	next := time.Now().Add(d.idle)
	if next.After(d.hard) {
		next = d.hard
	}
	if err := d.rc.SetReadDeadline(next); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return d.r.Read(p)
}
