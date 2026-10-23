// Package squel ;; implementation of sqlite :)
package squel

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Squel struct {
	db *sql.DB
}

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

// NewSquel ;; Returns a new instance of Squel
func NewSquel(ctx context.Context, path string) (*Squel, error) {
	s := Squel{}
	dsn, err := getdsn(path)
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

	return &s, nil
}

// ByeBye ;; Say goodbye to the *sql.DB connection!
func (s *Squel) ByeBye() error {
	return s.db.Close()
}
