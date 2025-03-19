local function define_migration()
    migration("Create documents and embeddings tables", function()
        database("sqlite", function()
            up(function(db)
                -- Create vector table for documents
                local success, err = db:execute([[
                    CREATE VIRTUAL TABLE IF NOT EXISTS documents USING vec0(
                        doc_id INTEGER PRIMARY KEY,
                        embedding float[1536],      -- Vector with 1536 dimensions
                        title TEXT,                 -- Document title
                        content TEXT,               -- Document content
                        created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
                    )
                ]])

                if err then
                    error(err)
                end

                -- Create full-text search table for text search
                success, err = db:execute([[
                    CREATE VIRTUAL TABLE IF NOT EXISTS doc_content USING fts5(
                        doc_id UNINDEXED,
                        title,
                        content
                    )
                ]])

                if err then
                    error(err)
                end

                return true
            end)

            down(function(db)
                -- Drop tables in reverse order of creation
                local success, err = db:execute("DROP TABLE IF EXISTS doc_content")
                if err then
                    error(err)
                end

                success, err = db:execute("DROP TABLE IF EXISTS documents")
                if err then
                    error(err)
                end

                return true
            end)
        end)
    end)
end

return require("migration").define(define_migration)
