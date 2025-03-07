local migration_db_proxy = {}
local sql = require("sql")

-- Create a proxy for DB connections to intercept and report SQL operations
function migration_db_proxy.create(db, reporter)
    local proxy = {}

    -- Wrap execute to capture SQL statements
    proxy.execute = function(_, sql_statement, params)
        local start_time = reporter.now()

        -- Report SQL execution
        reporter.report("sql:execute", {
            sql = sql_statement,
            params = params,
            timestamp = start_time
        })

        -- Execute the actual query
        local result, err = db:execute(sql_statement, params)

        local end_time = reporter.now()
        local duration = reporter.duration(start_time, end_time)

        -- Report results
        if err then
            reporter.report("sql:error", {
                message = "SQL execution failed: " .. err,
                sql = sql_statement,
                duration = duration,
                timestamp = end_time
            })
        else
            reporter.report("sql:success", {
                sql = sql_statement,
                duration = duration,
                rows_affected = result and result.rows_affected or 0,
                timestamp = end_time
            })
        end

        return result, err
    end

    -- Wrap query to capture SQL queries
    proxy.query = function(_, sql_query, params)
        local start_time = reporter.now()

        -- Report SQL query
        reporter.report("sql:query", {
            sql = sql_query,
            params = params,
            timestamp = start_time
        })

        -- Execute the actual query
        local result, err = db:query(sql_query, params)

        local end_time = reporter.now()
        local duration = reporter.duration(start_time, end_time)

        -- Report results
        if err then
            reporter.report("sql:error", {
                message = "SQL query failed: " .. err,
                sql = sql_query,
                duration = duration,
                timestamp = end_time
            })
        else
            reporter.report("sql:success", {
                sql = sql_query,
                duration = duration,
                rows = result and #result or 0,
                timestamp = end_time
            })
        end

        return result, err
    end

    -- Proxy for transaction begin
    proxy.begin = function()
        local tx, err = db:begin()
        if err then
            reporter.report("sql:error", {
                message = "Failed to begin transaction: " .. err,
                timestamp = reporter.now()
            })
            return nil, err
        end

        reporter.report("tx:begin", {
            timestamp = reporter.now()
        })

        -- Create a transaction proxy
        return migration_db_proxy.create(tx, reporter)
    end

    -- Proxy for transaction commit
    proxy.commit = function()
        reporter.report("tx:commit", {
            timestamp = reporter.now()
        })

        local success, err = db:commit()
        if not success then
            reporter.report("sql:error", {
                message = "Failed to commit transaction: " .. err,
                timestamp = reporter.now()
            })
        end

        return success, err
    end

    -- Proxy for transaction rollback
    proxy.rollback = function()
        reporter.report("tx:rollback", {
            timestamp = reporter.now()
        })

        local success, err = db:rollback()
        if not success then
            reporter.report("sql:error", {
                message = "Failed to rollback transaction: " .. err,
                timestamp = reporter.now()
            })
        end

        return success, err
    end

    -- Pass through database type
    proxy.type = function()
        return db:type()
    end

    -- Other DB methods
    proxy.prepare = function(_, sql_query)
        return db:prepare(sql_query)
    end

    proxy.release = function()
        return db:release()
    end

    -- Use a metatable to handle any other methods
    return setmetatable(proxy, {
        __index = function(_, key)
            return db[key]
        end
    })
end

-- Get a database connection with a proxy
function migration_db_proxy.get_connection(database_id, reporter)
    local db, err = sql.get(database_id)
    if err then
        reporter.report("error", {
            message = "Failed to connect to database: " .. err,
            timestamp = reporter.now()
        })
        return nil, err
    end

    return migration_db_proxy.create(db, reporter)
end

return migration_db_proxy
