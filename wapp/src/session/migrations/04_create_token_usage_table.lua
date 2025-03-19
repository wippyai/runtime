return require("migration").define(function()
    migration("Create token_usage table", function()
        database("sqlite", function()
            up(function(db)
                -- Create token_usage table
                local success, err = db:execute([[
                    CREATE TABLE token_usage (
                        usage_id TEXT PRIMARY KEY,
                        session_id TEXT NOT NULL,
                        model_name TEXT NOT NULL,
                        prompt_tokens INTEGER DEFAULT 0,
                        completion_tokens INTEGER DEFAULT 0,
                        thinking_tokens INTEGER DEFAULT 0,
                        total_tokens INTEGER DEFAULT 0,
                        timestamp INTEGER NOT NULL,
                        FOREIGN KEY (session_id) REFERENCES sessions(session_id)
                    )
                ]])

                if err then
                    return nil, "Failed to create token_usage table: " .. err
                end

                -- Create indexes
                success, err = db:execute("CREATE INDEX idx_token_usage_session ON token_usage(session_id)")
                if err then
                    return nil, "Failed to create session index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_token_usage_model ON token_usage(model_name)")
                if err then
                    return nil, "Failed to create model index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_token_usage_timestamp ON token_usage(timestamp)")
                if err then
                    return nil, "Failed to create timestamp index: " .. err
                end

                return true
            end)

            down(function(db)
                -- Drop indexes first
                local success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_session")
                if err then
                    return nil, "Failed to drop session index: " .. err
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_model")
                if err then
                    return nil, "Failed to drop model index: " .. err
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_timestamp")
                if err then
                    return nil, "Failed to drop timestamp index: " .. err
                end

                -- Drop table
                success, err = db:execute("DROP TABLE IF EXISTS token_usage")
                if err then
                    return nil, "Failed to drop token_usage table: " .. err
                end

                return true
            end)
        end)
    end)
end)