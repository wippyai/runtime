return require("migration").define(function()
    migration("Create file upload tables", function()
        database("sqlite", function()
            up(function(db)
                -- Create files table for upload metadata
                local success, err = db:execute([[
                    CREATE TABLE files (
                        file_id TEXT PRIMARY KEY,
                        user_id TEXT NOT NULL,
                        filename TEXT NOT NULL,
                        size INTEGER NOT NULL,
                        mime_type TEXT NOT NULL,
                        status TEXT NOT NULL DEFAULT 'processing',
                        storage_path TEXT NOT NULL,
                        created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        FOREIGN KEY (user_id) REFERENCES users(user_id)
                    )
                ]])

                if err then
                    error("Failed to create files table: " .. err)
                end

                -- Create file_content table to store the raw content
                success, err = db:execute([[
                    CREATE TABLE file_content (
                        content_id TEXT PRIMARY KEY,
                        file_id TEXT NOT NULL,
                        content BLOB NOT NULL,
                        FOREIGN KEY (file_id) REFERENCES files(file_id) ON DELETE CASCADE
                    )
                ]])

                if err then
                    error("Failed to create file_content table: " .. err)
                end

                -- Create file_sections table to store document structure
                success, err = db:execute([[
                    CREATE TABLE file_sections (
                        section_id TEXT PRIMARY KEY,
                        file_id TEXT NOT NULL,
                        title TEXT NOT NULL,
                        level INTEGER NOT NULL,
                        order_index INTEGER NOT NULL,
                        parent_id TEXT,
                        FOREIGN KEY (file_id) REFERENCES files(file_id) ON DELETE CASCADE,
                        FOREIGN KEY (parent_id) REFERENCES file_sections(section_id)
                    )
                ]])

                if err then
                    error("Failed to create file_sections table: " .. err)
                end

                -- Create file_chunks table for vectorized content pieces - UPDATED
                -- Using proper vec0 structure with partition keys and auxiliary columns
                -- Column order here matches the insert order in file_repo.add_chunk
                success, err = db:execute([[
                    CREATE VIRTUAL TABLE file_chunks USING vec0(
                        chunk_id TEXT PRIMARY KEY,
                        file_id TEXT PARTITION KEY,    -- Partition key for efficient filtering
                        section_id TEXT,               -- Metadata column for filtering
                        +content TEXT,                 -- Auxiliary column (retrieval only)
                        type TEXT,                     -- Metadata column for filtering
                        +path TEXT,                    -- Auxiliary column (retrieval only)
                        +created_at INTEGER,           -- Auxiliary column (retrieval only)
                        embedding float[512]           -- Vector column for semantic search
                    )
                ]])

                if err then
                    error("Failed to create file_chunks table: " .. err)
                end

                -- Create document_facts table for RAG responses
                success, err = db:execute([[
                    CREATE TABLE document_facts (
                        fact_id TEXT PRIMARY KEY,
                        file_id TEXT NOT NULL,
                        query TEXT NOT NULL,
                        original_response TEXT NOT NULL,
                        edited_response TEXT,
                        created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        FOREIGN KEY (file_id) REFERENCES files(file_id) ON DELETE CASCADE
                    )
                ]])

                if err then
                    error("Failed to create document_facts table: " .. err)
                end

                -- Create indexes for better performance (for regular tables only)
                success, err = db:execute("CREATE INDEX idx_files_user ON files(user_id)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_files_status ON files(status)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_file_sections_file ON file_sections(file_id)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_document_facts_file ON document_facts(file_id)")
                if err then
                    error(err)
                end
            end)

            down(function(db)
                -- Drop tables in reverse order of creation
                local tables = {
                    "document_facts",
                    "file_chunks",
                    "file_sections",
                    "file_content",
                    "files"
                }

                -- Drop indexes first (note: we don't need to drop indexes for vec0 tables)
                local indexes = {
                    "idx_document_facts_file",
                    "idx_file_sections_file",
                    "idx_files_status",
                    "idx_files_user"
                }

                for _, index_name in ipairs(indexes) do
                    local ok, err = db:execute("DROP INDEX IF EXISTS " .. index_name)
                    if err then
                        error("Failed to drop index " .. index_name .. ": " .. err)
                    end
                end

                for _, table_name in ipairs(tables) do
                    local ok, err = db:execute("DROP TABLE IF EXISTS " .. table_name)
                    if err then
                        error("Failed to drop table " .. table_name .. ": " .. err)
                    end
                end
            end)
        end)
    end)
end)