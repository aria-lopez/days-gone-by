// squel: implementation of sqlite :)
package squel

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Squel struct {
	db *sql.DB
}

// FIXME: [CONFIG] not hardcoded later.
// config stuff
func getdbpath() (string, error) {
	b, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(b, "days-gone-by")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "squel.db"), nil
}

func getdsn(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil
	}
	u := &url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(abs),
	}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(6000)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// NewSquel(): Returns a new instance of Squel
func NewSquel(ctx context.Context) (*Squel, error) {
	s := Squel{}
	dbPath, err := getdbpath()
	if err != nil {
		return nil, err
	}
	dsn, err := getdsn(dbPath)
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

// ByeBye(): Say goodbye to the *sql.DB connection!
func (s *Squel) ByeBye() error {
	return s.db.Close()
}
