package squeal

import (
	"context"
	"time"
)

// TouchKey ;; records that a key was just used
func (s *Squeal) TouchKey(ctx context.Context, id string) error {
	query := `
		UPDATE api_keys SET last_used_at = ? WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, time.Now().UnixMilli(), id)
	return err
}
