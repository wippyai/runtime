return require("migration").define(function()
    migration("Create session_contexts table", function()
        database("sqlite", function()
            up(function(db)
                -- Create session_contexts table (many-to-many relationship)
                local success, err = db:execute([[
                    CREATE TABLE session_contexts (
                        session_id TEXT NOT NULL,
                        context_id TEXT NOT NULL,
                        PRIMARY KEY (session_id, context_id),
                        FOREIGN KEY (session_id) REFERENCES sessions(session_id),
                        FOREIGN KEY (context_id) REFERENCES contexts(context_id)
                    )
                ]])

                if err then
                    return nil, "Failed to create session_contexts table: " .. err
                end

                -- Create index for faster lookups
                success, err = db:execute("CREATE INDEX idx_session_contexts_session ON session_contexts(session_id)")
                if err then
                    return nil, "Failed to create session index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_session_contexts_context ON session_contexts(context_id)")
                if err then
                    return nil, "Failed to create context index: " .. err
                end

                return true
            end)

            down(function(db)
                -- Drop indexes first
                local success, err = db:execute("DROP INDEX IF EXISTS idx_session_contexts_session")
                if err then
                    return nil, "Failed to drop session index: " .. err
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_session_contexts_context")
                if err then
                    return nil, "Failed to drop context index: " .. err
                end

                -- Drop table
                success, err = db:execute("DROP TABLE IF EXISTS session_contexts")
                if err then
                    return nil, "Failed to drop session_contexts table: " .. err
                end

                return true
            end)
        end)
    end)
end)