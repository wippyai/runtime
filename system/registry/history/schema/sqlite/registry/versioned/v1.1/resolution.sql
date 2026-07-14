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
