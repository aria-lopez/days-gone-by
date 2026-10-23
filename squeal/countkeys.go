package squeal

import "context"

// CountActiveKeys ;; how many keys can be used right now, and how many of those are admins
func (s *Squeal) CountActiveKeys(ctx context.Context) (total int, admins int, err error) {
	query := `
		SELECT count(*), coalesce(sum(role = 'admin'), 0) FROM api_keys WHERE revoked_at IS NULL
	`
	err = s.db.QueryRowContext(ctx, query).Scan(&total, &admins)
	return total, admins, err
}
