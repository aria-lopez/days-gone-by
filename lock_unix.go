//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// lock_unix; flock (file lock not the crazy company) manager incase you have multiple instances
// working with the same data

// lockDir ;; takes <dir>/days-gone-by.lock until the returned file is open, close on release
func lockDir(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "days-gone-by.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	// LOCK_NB call, this way a potential other instance doesnt wait it just fails
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		defer f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another days-gone-by%s is already using %s", lockHolder(f.Name()), dir)
		}
		return nil, err
	}
	// the syscall is just a call; we still have to lock it.
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// lockHolder ;; try to get lock holder (maybe this wont work always)
func lockHolder(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return ""
	}
	return " (pid " + strings.TrimSpace(string(b)) + ")"
}
