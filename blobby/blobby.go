// Package blobby its just a blob manager who is named blobby.
package blobby

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type Blobby struct {
	dir string
}

// utils
// syncDir ;; flushes a dir entrie to disk
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// path ;; gets path by sha key
func (b *Blobby) path(s string) string {
	return filepath.Join(b.dir, s[:2], s[2:4], s)
}

// validateSum ;; ensures given sha is 64chars so no one injects
func validateSum(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// NewBlobby ;; Get a new Blobby and mount it to a dir.
func NewBlobby(dir string) (*Blobby, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		return nil, err
	}
	return &Blobby{dir: dir}, nil
}

type BlobInfo struct {
	SHA256 string
	Size   int64
}

// Put ;; hand that file to Blobby but make sure its in allowedFormats
func (b *Blobby) Put(r io.Reader) (BlobInfo, error) {
	// create tmp
	tmp, err := os.CreateTemp(filepath.Join(b.dir, "tmp"), "upload-*")
	if err != nil {
		return BlobInfo{}, err
	}
	defer os.Remove(tmp.Name()) // this should be safe even once we rename?
	defer tmp.Close()

	// get hash and restore the prior 512 bytes
	// we write the bytes to the hash and file at the same time.
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return BlobInfo{}, err
	}
	// flush to disk before it gets a real name
	if err := tmp.Sync(); err != nil {
		return BlobInfo{}, err
	}
	// name is the hash
	sum := hex.EncodeToString(h.Sum(nil))
	destination := b.path(sum)
	info := BlobInfo{SHA256: sum, Size: size}

	// if its already there do nothing.
	if _, err := os.Stat(destination); err == nil {
		return info, err
	}

	// convert from temp to real.
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return BlobInfo{}, err
	}
	if err := os.Rename(tmp.Name(), destination); err != nil {
		return BlobInfo{}, err
	}

	// this could be expensive but as mentioned above removes fsycing
	// could be expiremental off if it needs high throughput
	leaf := filepath.Dir(destination)              // <b.dir>/ab/12 file name holder
	mid := filepath.Dir(leaf)                      // <b.dir>/ab holds hashed dir "12"
	for _, d := range []string{leaf, mid, b.dir} { // <b.dir> holds "ab"
		if err := syncDir(d); err != nil {
			return BlobInfo{}, err
		}
	}

	return info, nil
}

var ErrNotFound = errors.New("blob not found")

// Open ;; get a blob by its hash
func (b *Blobby) Open(s string) (*os.File, error) {
	if !validateSum(s) {
		return nil, ErrNotFound
	}
	f, err := os.Open(b.path(s))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

// Delete ;; removes a blob by its hash, ErrNotFound if its already gone
func (b *Blobby) Delete(s string) error {
	if !validateSum(s) {
		return ErrNotFound
	}
	p := b.path(s)
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	// make the removal durable, same reason as Put
	// we leave the empty ab/cd dirs alone, removing one could race a Put thats about to rename into it
	return syncDir(filepath.Dir(p))
}
