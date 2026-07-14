CREATE TABLE IF NOT EXISTS {{schema}}.resolution_graphs (
	digest TEXT PRIMARY KEY,
	data BYTEA NOT NULL
);

CREATE TABLE IF NOT EXISTS {{schema}}.version_resolutions (
	version_id BIGINT PRIMARY KEY REFERENCES {{schema}}.versions(id) ON DELETE CASCADE,
	resolution_digest TEXT NOT NULL REFERENCES {{schema}}.resolution_graphs(digest)
);
