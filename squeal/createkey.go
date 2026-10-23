package squeal

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CreateKey ;; stores a new api key by its hash, returns its id
func (s *Squeal) CreateKey(ctx context.Context, name string, role string, prefix string, hash string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	query := `
		INSERT INTO api_keys(id, name, key_hash, prefix, role, created_at)
									VALUES (?,  ?,    ?,        ?,      ?,    ?)
	`
	if _, err := s.db.ExecContext(ctx, query, id.String(), name, hash, prefix, role, time.Now().UnixMilli()); err != nil {
		return "", err
	}
	return id.String(), nil
}
