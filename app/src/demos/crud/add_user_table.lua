local function define_migration()
    migration("Create users table", function()
        database("sqlite", function()
            precondition(function(db)
                local result = db:query("SELECT name FROM sqlite_master WHERE type='table' AND name='users'")
                return not (result and #result > 0), "Users table already exists"
            end)

            up(function(db)
                local success, err = db:execute([[
                    CREATE TABLE users (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        username TEXT NOT NULL UNIQUE,
                        email TEXT NOT NULL UNIQUE,
                        password_hash TEXT NOT NULL,
                        full_name TEXT,
                        created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        active INTEGER NOT NULL DEFAULT 1
                    )
                ]])

                if err then
                    return nil, "Failed to create users table: " .. err
                end

                success, err = db:execute("CREATE INDEX idx_users_username ON users(username)")
                if err then
                    return nil, "Failed to create username index: " .. err
                end

                return true
            end)

            down(function(db)
                return db:execute("DROP TABLE IF EXISTS users")
            end)
        end)
    end)
end

return require("migration").define(define_migration)
