package squeal

import (
	"context"
	"database/sql"
	"errors"
)

type Image struct {
	ID        string  `json:"id"`
	SHA256    string  `json:"sha256"`
	Size      int64   `json:"size"`
	Mime      string  `json:"mime"`
	Filename  *string `json:"filename"`
	CreatedAt int64   `json:"created_at"`
	UpdatedAt int64   `json:"updated_at"`
}

func (s *Squeal) GetImage(ctx context.Context, id string) (Image, error) {
	query := `
		SELECT i.id, i.sha256, b.size, b.mime_type, i.filename, i.created_at, i.updated_at
		FROM images i
		JOIN blobs b ON b.sha256 = i.sha256
		WHERE i.id = ? AND i.deleted_at IS NULL
	`
	var img Image
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&img.ID,
		&img.SHA256,
		&img.Size,
		&img.Mime,
		&img.Filename,
		&img.CreatedAt,
		&img.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	return img, err
}
