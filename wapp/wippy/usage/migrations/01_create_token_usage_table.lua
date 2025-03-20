return require("migration").define(function()
    migration("Create token_usage table", function()
        database("sqlite", function()
            up(function(db)
                -- Create token_usage table
                local success, err = db:execute([[
                    CREATE TABLE token_usage (
                        usage_id TEXT PRIMARY KEY,
                        user_id TEXT NOT NULL,
                        context_id TEXT NULL,
                        model_name TEXT NOT NULL,
                        prompt_tokens INTEGER DEFAULT 0,
                        completion_tokens INTEGER DEFAULT 0,
                        thinking_tokens INTEGER DEFAULT 0,
                        total_tokens INTEGER DEFAULT 0,
                        timestamp INTEGER NOT NULL
                    )
                ]])

                if err then
                    error(err)
                end

                -- Create indexes
                success, err = db:execute("CREATE INDEX idx_token_usage_user ON token_usage(user_id)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_token_usage_context ON token_usage(context_id)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_token_usage_model ON token_usage(model_name)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_token_usage_timestamp ON token_usage(timestamp)")
                if err then
                    error(err)
                end
            end)

            down(function(db)
                -- Drop indexes first
                local success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_user")
                if err then
                    error("Failed to drop user index: " .. err)
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_context")
                if err then
                    error("Failed to drop context index: " .. err)
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_model")
                if err then
                    error("Failed to drop model index: " .. err)
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_token_usage_timestamp")
                if err then
                    error("Failed to drop timestamp index: " .. err)
                end

                -- Drop table
                success, err = db:execute("DROP TABLE IF EXISTS token_usage")
                if err then
                    error("Failed to drop token_usage table: " .. err)
                end
            end)
        end)
    end)
end)
