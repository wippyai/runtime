return require("migration").define(function()
    migration("Create users table", function()
        database("sqlite", function()
            up(function(db)
                local success, err = db:execute([[
                   CREATE TABLE users (
                       user_id TEXT PRIMARY KEY,
                       last_login INTEGER,
                       created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
                   )
               ]])

                if err then
                    error(err)
                end

                return true
            end)

            down(function(db)
                return db:execute("DROP TABLE IF EXISTS users")
            end)
        end)
    end)
end)
