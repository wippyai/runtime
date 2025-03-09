local function define_migration()
    migration("Create todos table", function()
        database("sqlite", function()
            precondition(function(db)
                local result = db:query("SELECT name FROM sqlite_master WHERE type='table' AND name='todos'")
                if result and #result > 0 then
                    return false, "Todos table already exists"
                end
                return true
            end)

            up(function(db)
                local success, err = db:execute([[
                    CREATE TABLE todos (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        title TEXT NOT NULL,
                        note TEXT,
                        created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
                        updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
                    )
                ]])

                if err then
                    return nil, "Failed to create todos table: " .. err
                end

                return true
            end)

            down(function(db)
                return db:execute("DROP TABLE IF EXISTS todos")
            end)
        end)
    end)
end

return require("migration").define(define_migration)
