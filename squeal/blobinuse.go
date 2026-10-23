package squeal

import "context"

// BlobInUse ;; true if any live file points at this blob
func (s *Squeal) BlobInUse(ctx context.Context, sha string) (bool, error) {
	var inUse bool
	query := `
		SELECT EXISTS (SELECT 1 FROM files WHERE sha256 = ? AND deleted_at IS NULL)
	`
	err := s.db.QueryRowContext(ctx, query, sha).Scan(&inUse)
	return inUse, err
}
