local sql = require("sql")
local logger = require("logger")
local crypto = require("crypto")

local function main()
    local db, err = sql.get("app:db")
    if err then
        logger:error("failed to connect", {error = tostring(err)})
        return 1
    end

    local _, exec_err = db:execute([[
        CREATE TABLE IF NOT EXISTS api_keys (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            api_key TEXT UNIQUE NOT NULL,
            user_id TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT 'user',
            created_at INTEGER NOT NULL
        )
    ]])

    if exec_err then
        db:release()
        logger:error("migration failed", {error = tostring(exec_err)})
        return 1
    end

    local rows, query_err = db:query("SELECT id FROM api_keys WHERE user_id = ?", {"demo"})
    if query_err then
        db:release()
        logger:error("query failed", {error = tostring(query_err)})
        return 1
    end

    if #rows == 0 then
        local demo_key, key_err = crypto.random.string(32)
        if key_err then
            db:release()
            logger:error("key generation failed", {error = tostring(key_err)})
            return 1
        end

        local _, insert_err = db:execute(
            "INSERT INTO api_keys (api_key, user_id, role, created_at) VALUES (?, ?, ?, ?)",
            {demo_key, "demo", "user", os.time()}
        )

        if insert_err then
            db:release()
            logger:error("insert failed", {error = tostring(insert_err)})
            return 1
        end

        logger:info("demo API key created", {api_key = demo_key})
    else
        local key_rows, _ = db:query("SELECT api_key FROM api_keys WHERE user_id = ?", {"demo"})
        if #key_rows > 0 then
            logger:info("demo API key exists", {api_key = key_rows[1].api_key})
        end
    end

    db:release()
    logger:info("migration complete")
    return 0
end

return { main = main }
