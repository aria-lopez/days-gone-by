package squeal

import (
	"context"
	"database/sql"
	"errors"
)

type File struct {
	ID        string  `json:"id"`
	SHA256    string  `json:"sha256"`
	Size      int64   `json:"size"`
	Mime      string  `json:"mime"`
	Filename  *string `json:"filename"`
	CreatedAt int64   `json:"created_at"`
	UpdatedAt int64   `json:"updated_at"`
}

func (s *Squeal) GetFile(ctx context.Context, id string) (File, error) {
	query := `
		SELECT f.id, f.sha256, b.size, b.mime_type, f.filename, f.created_at, f.updated_at
		FROM files f
		JOIN blobs b ON b.sha256 = f.sha256
		WHERE f.id = ? AND f.deleted_at IS NULL
	`
	var file File
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&file.ID,
		&file.SHA256,
		&file.Size,
		&file.Mime,
		&file.Filename,
		&file.CreatedAt,
		&file.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return file, err
}
