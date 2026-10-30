ALTER TABLE {{schema}}.versions ADD COLUMN lineage_digest TEXT;

CREATE TABLE IF NOT EXISTS {{schema}}.replay_checkpoints (
    version_id BIGINT PRIMARY KEY REFERENCES {{schema}}.versions(id) ON DELETE CASCADE,
    format_version INTEGER NOT NULL,
    digest TEXT NOT NULL,
    lineage_digest TEXT NOT NULL,
    data BYTEA NOT NULL
);
