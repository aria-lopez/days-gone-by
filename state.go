package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/squeal"
)

// State ;; application config
type State struct {
	defaultPath string         // ;; DAYS_GONE_BY_DIR         ;; where days-gone-by mounts too           ;; defaultPath()
	maxUpload   int64          // ;; DAYS_GONE_BY_MAX_UPLOAD  ;; max file size to be uploaded            ;; 50mib
	serverAddr  string         // ;; DAYS_GONE_BY_SERVER_ADDR ;; adress to launch server at [ip]:[port]  ;; 127.0.0.1:8080
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

	var maxUpload int64
	switch envMaxUp := os.Getenv("DAYS_GONE_BY_MAX_UPLOAD"); envMaxUp {
	case "":
		maxUpload = 50 << 20 // 50mib
	default:
		n, err := strconv.ParseInt(envMaxUp, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad DAYS_GONE_BY_MAX_UPLOAD=%q: %w", envMaxUp, err)
		}
		if n <= 0 {
			return nil, fmt.Errorf("bad DAYS_GONE_BY_MAX_UPLOAD must be positive, got %d", n)
		}
		maxUpload = n
	}
	s.maxUpload = maxUpload

	var serverAddr string
	switch envServerAddr := os.Getenv("DAYS_GONE_BY_SERVER_ADDR"); envServerAddr {
	case "":
		serverAddr = "127.0.0.1:8080"
	default:
		serverAddr = envServerAddr
	}
	s.serverAddr = serverAddr

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
func (s *State) DBpath() string     { return filepath.Join(s.defaultPath, "squel.db") }
func (s *State) Blobbypath() string { return filepath.Join(s.defaultPath, "blobs") }

// Close ;; Cleans up anything required at destruction of app.
func (s *State) Close() {
	s.squeal.ByeBye()
	s.lock.Close()
}
