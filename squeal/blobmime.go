package squeal

import (
	"context"
	"database/sql"
	"errors"
)

// BlobMime ;; attempts to get stored blob type by sha
func (s *Squeal) BlobMime(ctx context.Context, sha string) (string, error) {
	var m string
	query := `
		SELECT mime_type FROM blobs WHERE sha256 = ? AND purged_at IS NULL
	`
	err := s.db.QueryRowContext(ctx, query, sha).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return m, err
}
