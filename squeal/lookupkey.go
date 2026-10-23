package squeal

import (
	"context"
	"database/sql"
	"errors"
)

// LookupKey ;; the active key with this hash, ErrNotFound if there is none or it was revoked
func (s *Squeal) LookupKey(ctx context.Context, hash string) (Key, error) {
	query := `
		SELECT id, name, prefix, role, created_at FROM api_keys
		WHERE key_hash = ? AND revoked_at IS NULL
	`
	var k Key
	err := s.db.QueryRowContext(ctx, query, hash).Scan(&k.ID, &k.Name, &k.Prefix, &k.Role, &k.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, ErrNotFound
	}
	return k, err
}
