package squeal

import (
	"context"
	"fmt"
	"strings"

	"github.com/aria-lopez/days-gone-by/logger"
)

// schemaVersion ;; bump when schema.go is updated, and add the migration from the old one.
// this is kinda a bad way of handling this but the schema shouldnt change often.
const schemaVersion = 2

// migrations ;; migrations[0] takes a version 1 db to version 2, [1] takes 2 to 3 and so on.
// each one has to leave the db looking exactly like schema.go would have made it from scratch
var migrations = []string{
	// v2; add api_keys
	`
	CREATE TABLE api_keys (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		key_hash     TEXT NOT NULL UNIQUE CHECK (length(key_hash) = 64),
		prefix       TEXT NOT NULL,
		role         TEXT NOT NULL CHECK (role IN ('viewer', 'member', 'admin')),
		created_at   INTEGER NOT NULL,
		last_used_at INTEGER,
		revoked_at   INTEGER
	) STRICT;
	ALTER TABLE images ADD COLUMN created_by TEXT REFERENCES api_keys(id);
	`,
}

// checkVersion ;; make sure live db is up to date.
func (s *Squeal) checkVersion(ctx context.Context) error {
	if len(migrations) != schemaVersion-1 {
		return fmt.Errorf("bug: schemaVersion is %d but there are %d migrations", schemaVersion, len(migrations))
	}

	var version, tables int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	query := `
		SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
	`
	if err := s.db.QueryRowContext(ctx, query).Scan(&tables); err != nil {
		return err
	}

	switch {
	case version == 0 && tables == 0:
		return s.create(ctx)
	case version == 0:
		return fmt.Errorf("database has tables but no schema version, it came from a development build, move it away or delete it")
	case version > schemaVersion:
		return fmt.Errorf("database is schema version %d but this build only knows version %d, upgrade days-gone-by", version, schemaVersion)
	case version < schemaVersion:
		return s.migrate(ctx, version)
	}
	return nil
}

// create ;; schema and version in one transaction so a crash cant leave tables without a version
func (s *Squeal) create(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// migrate ;; bring old db up to current version
func (s *Squeal) migrate(ctx context.Context, from int) error {
	backup := fmt.Sprintf("%s.before-v%d", s.path, schemaVersion)
	into := fmt.Sprintf(`VACUUM INTO '%s'`, strings.ReplaceAll(backup, "'", "''"))
	if _, err := s.db.ExecContext(ctx, into); err != nil {
		return fmt.Errorf("couldnt back up the database before migrating it (is %s already there?): %w", backup, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for v := from; v < schemaVersion; v++ {
		if _, err := tx.ExecContext(ctx, migrations[v-1]); err != nil {
			return fmt.Errorf("migrating database from version %d to %d: %w", v, v+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	logger.InfoF("migrated database from version %d to %d, copy of the old one is at %s", from, schemaVersion, backup)
	return nil
}
