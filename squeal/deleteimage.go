package squeal

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DeleteImage ;; tombstones an image and returns the sha of its blob,
// ErrNotFound if it never existed or is already deleted
func (s *Squeal) DeleteImage(ctx context.Context, id string) (string, error) {
	now := time.Now().UnixMilli()
	query := `
		UPDATE images SET deleted_at = ?, updated_at = ?
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
