package squeal

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RevokeKey ;; revokes an active key. ErrNotFound if there is no active key with that id,
// ErrLastAdmin if it is the only active admin key left
func (s *Squeal) RevokeKey(ctx context.Context, id string) error {
	// one statement so the last admin check and the revoke cant be split by someone else
	query := `
		UPDATE api_keys SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL
		AND (role != 'admin' OR EXISTS (
			SELECT 1 FROM api_keys o WHERE o.role = 'admin' AND o.revoked_at IS NULL AND o.id != api_keys.id
		))
	`
	res, err := s.db.ExecContext(ctx, query, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	// nothing changed, find out why so the caller can say so
	var role string
	query = `
		SELECT role FROM api_keys WHERE id = ? AND revoked_at IS NULL
	`
	err = s.db.QueryRowContext(ctx, query, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role != "admin" {
		return ErrNotFound // somebody revoked it between our two statements
	}
	return ErrLastAdmin
}
