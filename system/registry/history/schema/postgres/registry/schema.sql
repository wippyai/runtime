CREATE TABLE IF NOT EXISTS {{schema}}.metadata (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS {{schema}}.versions (
	id BIGINT PRIMARY KEY,
	parent_id BIGINT REFERENCES {{schema}}.versions(id)
);

CREATE TABLE IF NOT EXISTS {{schema}}.changesets (
	version_id BIGINT PRIMARY KEY REFERENCES {{schema}}.versions(id) ON DELETE CASCADE,
	data BYTEA NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_versions_parent ON {{schema}}.versions(parent_id);
