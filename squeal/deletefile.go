package squeal

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DeleteFile ;; tombstones a file and returns the sha of its blob,
// ErrNotFound if it never existed or is already deleted
func (s *Squeal) DeleteFile(ctx context.Context, id string) (string, error) {
	now := time.Now().UnixMilli()
	query := `
		UPDATE files SET deleted_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
		RETURNING sha256
	`
	var sha string
	err := s.db.QueryRowContext(ctx, query, now, now, id).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return sha, err
}
