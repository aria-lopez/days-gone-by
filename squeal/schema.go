package squeal

// schema ;; always the NEWEST version, a brand new db is created straight from this.
// older dbs are brought forward by migrations in version.go, keep the two in step

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

CREATE TABLE IF NOT EXISTS api_keys (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL,
	key_hash     TEXT NOT NULL UNIQUE CHECK (length(key_hash) = 64), -- sha256 of the key, the key itself is never stored
	prefix       TEXT NOT NULL,                                       -- first chars of the key, a public label
	role         TEXT NOT NULL CHECK (role IN ('viewer', 'member', 'admin')),
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER,
	revoked_at   INTEGER -- NULL = active, keys are revoked never deleted
) STRICT;

CREATE TABLE IF NOT EXISTS files (
	id          TEXT PRIMARY KEY,
	sha256      TEXT NOT NULL REFERENCES blobs(sha256),
	origin_node TEXT NOT NULL,
	filename    TEXT,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	deleted_at  INTEGER, -- NULL = live, set = tombstone
	created_by  TEXT REFERENCES api_keys(id) -- key that uploaded it, NULL = from before keys existed. last because ALTER TABLE can only add at the end
) STRICT;

CREATE INDEX IF NOT EXISTS files_sha256 ON files(sha256);
`
