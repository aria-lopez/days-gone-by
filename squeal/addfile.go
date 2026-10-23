package squeal

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AddFile ;; record to db after Blobby.Put(), createdBy is the id of the api key that uploaded it
func (s *Squeal) AddFile(ctx context.Context, sha string, size int64, mime string, filename string, createdBy string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	now := time.Now().UnixMilli()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO blobs(sha256, size, mime_type, created_at)
								VALUES (?,      ?,    ?,         ?)
		ON CONFLICT(sha256) DO UPDATE SET purged_at = NULL
	`
	if _, err := tx.ExecContext(ctx, query, sha, size, mime, now); err != nil {
		return "", err
	}
	var fn any
	if filename != "" {
		fn = filename
	}
	var by any // NULL when we dont know, '' would break the foreign key
	if createdBy != "" {
		by = createdBy
	}
	// no OR IGNORE, a skipped row would still hand back an id for a file that doesnt exist
	query = `
		INSERT INTO files(id, sha256, origin_node, filename, created_by, created_at, updated_at)
								 VALUES (?,  ?,      ?,           ?,        ?,          ?,          ?)
	`
	if _, err := tx.ExecContext(ctx, query, id.String(), sha, s.nodeID, fn, by, now, now); err != nil {
		return "", err
	}
	return id.String(), tx.Commit()
}
