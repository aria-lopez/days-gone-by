package squeal

// will worry about migrations later.

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key    TEXT PRIMARY KEY,
	value  TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS blobs (
	sha256     TEXT PRIMARY KEY CHECK (length(sha256) = 64),
	size       INTEGER NOT NULL,
	mime_type  TEXT NOT NULL, -- type of file
	created_at INTEGER NOT NULL,
	purged_at  INTEGER        -- NULL = bytes should be on disk, set = bytes removed
) STRICT, WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS images (
	id          TEXT PRIMARY KEY,
	sha256      TEXT NOT NULL REFERENCES blobs(sha256),
	origin_node TEXT NOT NULL,
	filename    TEXT,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	deleted_at  INTEGER -- NULL = live, set = tombstone
) STRICT;

CREATE INDEX IF NOT EXISTS images_sha256 ON images(sha256);
`
