local http = require("http")
local json = require("json")
local sql = require("sql")

-- Function to check available tables in a database
local function check_tables()
    -- Set up HTTP response
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to create HTTP context"
    end

    -- Set response headers
    res:set_status(http.STATUS.OK)
    res:set_content_type(http.CONTENT.JSON)
    res:set_header("Access-Control-Allow-Origin", "*")
    res:set_header("Access-Control-Allow-Methods", "GET")

    -- Get target database from query parameters (required)
    local target_db = req:query("target_db")
    if not target_db or target_db == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required parameter: target_db"
        })
        return true
    end

    -- Get database connection
    local db, err = sql.get(target_db)
    if err then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({
            error = "Failed to connect to database: " .. tostring(err)
        })
        return true
    end

    -- Get database type
    local db_type, type_err = db:type()
    if type_err then
        db:release()
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({
            error = "Failed to determine database type: " .. tostring(type_err)
        })
        return true
    end

    -- Query for tables based on database type
    local tables = {}
    local query_err

    if db_type == "sqlite" then
        tables, query_err = db:query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
    elseif db_type == "postgres" then
        tables, query_err = db:query(
        "SELECT tablename as name FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename")
    elseif db_type == "mysql" then
        tables, query_err = db:query(
        "SELECT table_name as name FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name")
    else
        db:release()
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({
            error = "Unsupported database type: " .. db_type
        })
        return true
    end

    -- Release database connection
    db:release()

    -- Handle query errors
    if query_err then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({
            error = "Failed to query tables: " .. tostring(query_err)
        })
        return true
    end

    -- Extract table names
    local table_names = {}
    for _, table in ipairs(tables or {}) do
        table.insert(table_names, table.name)
    end

    -- Return the discovered tables
    res:write_json({
        database = target_db,
        db_type = db_type,
        tables = table_names,
        count = #table_names
    })

    return true
end

return { check_tables = check_tables }
