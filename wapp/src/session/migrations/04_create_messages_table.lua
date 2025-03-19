return require("migration").define(function()
    migration("Create messages table", function()
        database("sqlite", function()
            up(function(db)
                -- Create messages table
                local success, err = db:execute([[
                    CREATE TABLE messages (
                        message_id TEXT PRIMARY KEY,
                        session_id TEXT NOT NULL,
                        date INTEGER NOT NULL,
                        type TEXT NOT NULL,
                        data BLOB NOT NULL,
                        metadata TEXT,
                        FOREIGN KEY (session_id) REFERENCES sessions(session_id)
                    )
                ]])

                if err then
                    error(err)
                end

                -- Create indexes
                success, err = db:execute("CREATE INDEX idx_messages_session_date ON messages(session_id, date)")
                if err then
                    error(err)
                end

                success, err = db:execute("CREATE INDEX idx_messages_type ON messages(type)")
                if err then
                    error(err)
                end
            end)

            down(function(db)
                -- Drop indexes first
                local success, err = db:execute("DROP INDEX IF EXISTS idx_messages_session_date")
                if err then
                    error(err)
                end

                success, err = db:execute("DROP INDEX IF EXISTS idx_messages_type")
                if err then
                    error(err)
                end

                -- Drop table
                success, err = db:execute("DROP TABLE IF EXISTS messages")
                if err then
                    error(err)
                end
            end)
        end)
    end)
end)
