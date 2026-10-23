package squeal

import "context"

// ListKeys ;; every key, oldest first, revoked ones included
func (s *Squeal) ListKeys(ctx context.Context) ([]Key, error) {
	query := `
		SELECT id, name, prefix, role, created_at, last_used_at, revoked_at FROM api_keys ORDER BY id
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Role, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
