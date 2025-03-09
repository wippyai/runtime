local function define_migration()
    migration("Create todo_items table", function()
        database("sqlite", function()
            precondition(function(db)
                -- First check if the table already exists
                local result = db:query("SELECT name FROM sqlite_master WHERE type='table' AND name='todo_items'")
                if result and #result > 0 then
                    return false, "Todo items table already exists"
                end

                -- Check if users table exists (since we need a foreign key reference)
                local users_result = db:query("SELECT name FROM sqlite_master WHERE type='table' AND name='users'")
                if not users_result or #users_result == 0 then
                    return false, "Users table does not exist yet - required for foreign key"
                end

                return true
            end)

            up(function(db)
                local success, err = db:execute([[
                    CREATE TABLE todo_items (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        user_id INTEGER NOT NULL,
                        title TEXT NOT NULL,
                        description TEXT,
                        due_date INTEGER,
                        priority INTEGER NOT NULL DEFAULT 0,
                        completed INTEGER NOT NULL DEFAULT 0,
                        created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
                    )
                ]])

                if err then
                    return nil, "Failed to create todo_items table: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_todo_user_id ON todo_items(user_id)")
                if err then
                    return nil, "Failed to create user_id index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_todo_due_date ON todo_items(due_date)")
                if err then
                    return nil, "Failed to create due_date index: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_todo_completed ON todo_items(completed)")
                if err then
                    return nil, "Failed to create completed index: " .. err
                end

                return true
            end)

            down(function(db)
                return db:execute("DROP TABLE IF EXISTS todo_items")
            end)
        end)
    end)
end

return require("migration").define(define_migration)
