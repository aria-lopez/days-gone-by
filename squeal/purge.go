package squeal

import (
	"context"
	"time"
)

// PurgeCandidates ;; sha of every blob that still has bytes but no live image
func (s *Squeal) PurgeCandidates(ctx context.Context) ([]string, error) {
	query := `
		SELECT b.sha256 FROM blobs b
		WHERE b.purged_at IS NULL
		AND NOT EXISTS (
			SELECT 1 FROM images i WHERE i.sha256 = b.sha256 AND i.deleted_at IS NULL
		)
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	// we only have 1 conn so rows has to be closed before anyone runs another query
	defer rows.Close()

	var shas []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		shas = append(shas, sha)
	}
	return shas, rows.Err()
}

// MarkPurged ;; records that a blobs bytes are gone from disk
func (s *Squeal) MarkPurged(ctx context.Context, sha string) error {
	query := `
		UPDATE blobs SET purged_at = ? WHERE sha256 = ? AND purged_at IS NULL
	`
	_, err := s.db.ExecContext(ctx, query, time.Now().UnixMilli(), sha)
	return err
}
