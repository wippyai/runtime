ALTER TABLE versions ADD COLUMN lineage_digest TEXT;

CREATE TABLE IF NOT EXISTS replay_checkpoints (
    version_id INTEGER PRIMARY KEY REFERENCES versions(id) ON DELETE CASCADE,
    format_version INTEGER NOT NULL,
    digest TEXT NOT NULL,
    lineage_digest TEXT NOT NULL,
    data BLOB NOT NULL
);
