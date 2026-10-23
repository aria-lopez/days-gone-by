package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/config"
	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/squeal"
)

// State ;; application state
type State struct {
	defaultPath string         // ;; DAYS_GONE_BY_DIR ;; where days-gone-by mounts too ;; defaultPath()
	cfg         config.Config  // ;; everything else is a setting, see package config
	store       *blobby.Blobby // ;;
	squeal      *squeal.Squeal // ;;
	lock        *os.File       // ;;
}

// NewState ;; return a new instance of State while trying to parse config
// error on invalid config
func NewState(ctx context.Context) (_ *State, err error) {
	s := &State{}
	dp, err := defaultPath()
	if err != nil {
		return nil, err
	}
	s.defaultPath = dp

	cfg, err := config.Load(dp)
	if err != nil {
		return nil, err
	}
	s.cfg = cfg

	if err := os.MkdirAll(dp, 0o700); err != nil {
		return nil, err
	}

	lock, err := lockDir(dp)
	if err != nil {
		return nil, err
	}
	s.lock = lock
	// any failure below hands everything back, db before the lock like Close()
	// err is a named result so this sees every return, even the ones from inner scopes
	defer func() {
		if err != nil {
			if s.squeal != nil {
				s.squeal.ByeBye()
			}
			lock.Close()
		}
	}()

	store, err := blobby.NewBlobby(s.Blobbypath())
	if err != nil {
		return nil, err
	}
	s.store = store

	// we hold the lock and arent serving yet so nothing in tmp can be a live upload
	if n, err := store.CleanTmp(); err != nil {
		return nil, err
	} else if n > 0 {
		logger.InfoF("cleaned %d leftover upload files out of tmp", n)
	}

	db, err := squeal.NewSqueal(ctx, s.DBpath())
	if err != nil {
		return nil, err
	}
	s.squeal = db

	return s, nil
}

// defaultPath ;; determine where to mount days-gone-by
func defaultPath() (string, error) {
	if p := os.Getenv("DAYS_GONE_BY_DIR"); p != "" {
		// if there is an explicit dir use that.
		return filepath.Abs(p)
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		// Linux if XDG_DATA_HOME is set
		return filepath.Join(x, "days-gone-by"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		// I think this is how to handle darwin
		return filepath.Join(home, "Library", "Application Support", "days-gone-by"), nil
	}
	// otherwise just use ~/.local/share
	return filepath.Join(home, ".local", "share", "days-gone-by"), nil
}

// Path formaters
// dbPath ;; where the db lives inside a data dir, the cli commands need it without a whole State
func dbPath(root string) string     { return filepath.Join(root, "squel.db") }
func (s *State) DBpath() string     { return dbPath(s.defaultPath) }
func (s *State) Blobbypath() string { return filepath.Join(s.defaultPath, "blobs") }

// Close ;; Cleans up anything required at destruction of app.
func (s *State) Close() {
	s.squeal.ByeBye()
	s.lock.Close()
}
