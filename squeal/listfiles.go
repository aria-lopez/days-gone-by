package squeal

import "context"

// ListFiles ;; live files, newest first. after is the id to continue from ("" = the start),
// sha limits it to one blob ("" = any)
func (s *Squeal) ListFiles(ctx context.Context, after string, sha string, limit int) ([]File, error) {
	var cursor, hash any // NULL when not given
	if after != "" {
		cursor = after
	}
	if sha != "" {
		hash = sha
	}
	// ids are UUIDv7 so id order is upload order, and "id < after" keeps pages stable while files are added
	query := `
		SELECT f.id, f.sha256, b.size, b.mime_type, f.filename, f.created_at, f.updated_at
		FROM files f
		JOIN blobs b ON b.sha256 = f.sha256
		WHERE f.deleted_at IS NULL
		AND (?1 IS NULL OR f.id < ?1)
		AND (?2 IS NULL OR f.sha256 = ?2)
		ORDER BY f.id DESC
		LIMIT ?3
	`
	rows, err := s.db.QueryContext(ctx, query, cursor, hash, limit)
	if err != nil {
		return nil, err
	}
	// we only have 1 conn so rows has to be closed before anyone runs another query
	defer rows.Close()

	files := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.SHA256, &f.Size, &f.Mime, &f.Filename, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
