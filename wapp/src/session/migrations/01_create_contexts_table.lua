return require("migration").define(function()
    migration("Create contexts table", function()
        database("sqlite", function()
            up(function(db)
                -- Create contexts table
                local success, err = db:execute([[
                    CREATE TABLE contexts (
                        context_id TEXT PRIMARY KEY, -- This is the UUID
                        type TEXT NOT NULL,
                        data BLOB
                    )
                ]])

                if err then
                    return nil, "Failed to create contexts table: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_contexts_type ON contexts(type)")
                if err then
                    return nil, "Failed to create type index: " .. err
                end

                return true
            end)

            down(function(db)
                -- Drop index first
                local success, err = db:execute("DROP INDEX IF EXISTS idx_contexts_type")
                if err then
                    return nil, "Failed to drop type index: " .. err
                end

                -- Drop table
                success, err = db:execute("DROP TABLE IF EXISTS contexts")
                if err then
                    return nil, "Failed to drop contexts table: " .. err
                end

                return true
            end)
        end)
    end)
end)