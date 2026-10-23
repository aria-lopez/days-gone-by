// Package blobby its just a blob manager who is named blobby.
package blobby

import (
	"crypto/sha256"
	"encoding/hex"
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
	// make some leafs so not everything is in 1 dir
	// its still deterministic since sum is the key
	// in theory even at 100 million files we would end up with ~1,500 per leaf
	destination := filepath.Join(b.dir, sum[:2], sum[2:4], sum)
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
