return require("migration").define(function()
    migration("Create sessions table", function()
        database("sqlite", function()
            up(function(db)
                -- Create sessions table
                local success, err = db:execute([[
                    CREATE TABLE sessions (
                        session_id TEXT PRIMARY KEY,
                        user_id TEXT NOT NULL,
                        primary_context_id TEXT NOT NULL,
                        title TEXT DEFAULT '',
                        start_date INTEGER NOT NULL,
                        last_message_date INTEGER,
                        FOREIGN KEY (user_id) REFERENCES users(user_id),
                        FOREIGN KEY (primary_context_id) REFERENCES contexts(context_id)
                    )
                ]])

                if err then
                    return nil, "Failed to create sessions table: " .. err
                end

                -- Create indexes
                success, err = db:execute("CREATE INDEX idx_sessions_user ON sessions(user_id)")
                if err then
                    return nil, "Failed to create user index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_sessions_date ON sessions(start_date)")
                if err then
                    return nil, "Failed to create date index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_sessions_primary_context ON sessions(primary_context_id)")
                if err then
                    return nil, "Failed to create primary context index: " .. err
                end

                return true
            end)

            down(function(db)
                -- Drop indexes first
                local success, err = db:execute("DROP INDEX IF EXISTS idx_sessions_user")
                if err then
                    return nil, "Failed to drop user index: " .. err
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_sessions_date")
                if err then
                    return nil, "Failed to drop date index: " .. err
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_sessions_primary_context")
                if err then
                    return nil, "Failed to drop primary context index: " .. err
                end

                -- Drop table
                success, err = db:execute("DROP TABLE IF EXISTS sessions")
                if err then
                    return nil, "Failed to drop sessions table: " .. err
                end

                return true
            end)
        end)
    end)
end)
