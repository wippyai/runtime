CREATE TABLE IF NOT EXISTS metadata (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS versions (
	id INTEGER PRIMARY KEY,
	parent_id INTEGER,
	FOREIGN KEY (parent_id) REFERENCES versions(id)
);

CREATE TABLE IF NOT EXISTS changesets (
	version_id INTEGER PRIMARY KEY,
	data BLOB NOT NULL,
	FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_versions_parent ON versions(parent_id);

CREATE TABLE IF NOT EXISTS resolution_graphs (
	digest TEXT PRIMARY KEY,
	data BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS version_resolutions (
	version_id INTEGER PRIMARY KEY,
	resolution_digest TEXT NOT NULL,
	FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE,
	FOREIGN KEY (resolution_digest) REFERENCES resolution_graphs(digest)
);
