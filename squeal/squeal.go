// Package squeal ;; implementation of sqlite :)
package squeal

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Squeal struct {
	db     *sql.DB
	nodeID string
	path   string // ;; absolute path of the db file, migrations back it up next to itself
}

// utils
func getdsn(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	u := &url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(abs),
	}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(6000)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// NewSqueal ;; Returns a new instance of Squeal
func NewSqueal(ctx context.Context, path string) (*Squeal, error) {
	s := Squeal{}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s.path = abs
	dsn, err := getdsn(abs)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s.db = db

	if err := db.PingContext(ctx); err != nil {
		s.db.Close()
		return nil, err
	}
	if err := s.init(ctx); err != nil {
		s.db.Close()
		return nil, err
	}

	return &s, nil
}

func (s *Squeal) init(ctx context.Context) error {
	if err := s.checkVersion(ctx); err != nil {
		return err
	}
	// seed meta for node
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO meta(key, value) VALUES ('node_id', ?)`, uuid.NewString()); err != nil {
		return err
	}
	return s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'node_id'`).Scan(&s.nodeID)
}

// ByeBye ;; Say goodbye to the *sql.DB connection!
func (s *Squeal) ByeBye() error {
	return s.db.Close()
}
