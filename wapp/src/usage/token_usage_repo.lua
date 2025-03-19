local sql = require("sql")
local uuid = require("uuid")

-- Hardcoded database resource name
local DB_RESOURCE = "app:db"

local token_usage_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Record token usage
function token_usage_repo.record(user_id, context_id, model_name, prompt_tokens, completion_tokens, thinking_tokens)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    if not model_name or model_name == "" then
        return nil, "Model name is required"
    end

    -- Default token counts to 0 if not provided
    prompt_tokens = prompt_tokens or 0
    completion_tokens = completion_tokens or 0
    thinking_tokens = thinking_tokens or 0

    -- Calculate total tokens
    local total_tokens = prompt_tokens + completion_tokens + thinking_tokens

    -- Generate a new UUID for this usage record
    local usage_id = uuid.v7()

    local db, err = get_db()
    if err then
        return nil, err
    end

    local now = os.time()

    local result, err = db:execute(
        [[INSERT INTO token_usage
          (usage_id, user_id, context_id, model_name, prompt_tokens, completion_tokens, thinking_tokens, total_tokens, timestamp)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)]],
        {
            usage_id,
            user_id,
            context_id, -- can be nil
            model_name,
            sql.as.int(prompt_tokens),
            sql.as.int(completion_tokens),
            sql.as.int(thinking_tokens),
            sql.as.int(total_tokens),
            sql.as.int(now)
        }
    )

    db:release()

    if err then
        return nil, "Failed to record token usage: " .. err
    end

    return {
        usage_id = usage_id,
        user_id = user_id,
        context_id = context_id,
        model_name = model_name,
        prompt_tokens = prompt_tokens,
        completion_tokens = completion_tokens,
        thinking_tokens = thinking_tokens,
        total_tokens = total_tokens,
        timestamp = now
    }
end

-- Get token usage by ID
function token_usage_repo.get(usage_id)
    if not usage_id or usage_id == "" then
        return nil, "Usage ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT usage_id, user_id, context_id, model_name, prompt_tokens, completion_tokens,
               thinking_tokens, total_tokens, timestamp
        FROM token_usage
        WHERE usage_id = ?
    ]]

    local usages, err = db:query(query, { usage_id })
    db:release()

    if err then
        return nil, "Failed to get token usage: " .. err
    end

    if #usages == 0 then
        return nil, "Token usage record not found"
    end

    return usages[1]
end

-- Get all token usage for a user
function token_usage_repo.get_by_user(user_id, start_time, end_time)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { user_id }
    local time_condition = ""

    -- Add time filters if provided
    if start_time then
        time_condition = time_condition .. " AND timestamp >= ?"
        table.insert(params, sql.as.int(start_time))
    end

    if end_time then
        time_condition = time_condition .. " AND timestamp <= ?"
        table.insert(params, sql.as.int(end_time))
    end

    local query = [[
        SELECT usage_id, user_id, context_id, model_name, prompt_tokens, completion_tokens,
               thinking_tokens, total_tokens, timestamp
        FROM token_usage
        WHERE user_id = ?]] .. time_condition .. [[
        ORDER BY timestamp ASC
    ]]

    local usages, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to get token usage for user: " .. err
    end

    return usages
end

-- Get all token usage for a context
function token_usage_repo.get_by_context(context_id, start_time, end_time)
    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { context_id }
    local time_condition = ""

    -- Add time filters if provided
    if start_time then
        time_condition = time_condition .. " AND timestamp >= ?"
        table.insert(params, sql.as.int(start_time))
    end

    if end_time then
        time_condition = time_condition .. " AND timestamp <= ?"
        table.insert(params, sql.as.int(end_time))
    end

    local query = [[
        SELECT usage_id, user_id, context_id, model_name, prompt_tokens, completion_tokens,
               thinking_tokens, total_tokens, timestamp
        FROM token_usage
        WHERE context_id = ?]] .. time_condition .. [[
        ORDER BY timestamp ASC
    ]]

    local usages, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to get token usage for context: " .. err
    end

    return usages
end

-- Get aggregated token usage for a context
function token_usage_repo.get_context_totals(context_id)
    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT
            SUM(prompt_tokens) as prompt_tokens,
            SUM(completion_tokens) as completion_tokens,
            SUM(thinking_tokens) as thinking_tokens,
            SUM(total_tokens) as total_tokens,
            COUNT(*) as request_count
        FROM token_usage
        WHERE context_id = ?
    ]]

    local result, err = db:query(query, { context_id })
    db:release()

    if err then
        return nil, "Failed to get context token totals: " .. err
    end

    -- Return zeros if no records found
    if not result[1].prompt_tokens then
        return {
            context_id = context_id,
            prompt_tokens = 0,
            completion_tokens = 0,
            thinking_tokens = 0,
            total_tokens = 0,
            request_count = 0
        }
    end

    result[1].context_id = context_id
    return result[1]
end

-- Get token usage by model
function token_usage_repo.get_by_model(model_name, start_time, end_time)
    if not model_name or model_name == "" then
        return nil, "Model name is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { model_name }
    local time_condition = ""

    -- Add time filters if provided
    if start_time then
        time_condition = time_condition .. " AND timestamp >= ?"
        table.insert(params, sql.as.int(start_time))
    end

    if end_time then
        time_condition = time_condition .. " AND timestamp <= ?"
        table.insert(params, sql.as.int(end_time))
    end

    local query = [[
        SELECT
            model_name,
            SUM(prompt_tokens) as prompt_tokens,
            SUM(completion_tokens) as completion_tokens,
            SUM(thinking_tokens) as thinking_tokens,
            SUM(total_tokens) as total_tokens,
            COUNT(*) as request_count
        FROM token_usage
        WHERE model_name = ?]] .. time_condition .. [[
        GROUP BY model_name
    ]]

    local result, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to get model token usage: " .. err
    end

    -- Return zeros if no records found
    if #result == 0 then
        return {
            model_name = model_name,
            prompt_tokens = 0,
            completion_tokens = 0,
            thinking_tokens = 0,
            total_tokens = 0,
            request_count = 0
        }
    end

    return result[1]
end

-- Get aggregated token usage for a user
function token_usage_repo.get_user_totals(user_id, start_time, end_time)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { user_id }
    local time_condition = ""

    -- Add time filters if provided
    if start_time then
        time_condition = time_condition .. " AND timestamp >= ?"
        table.insert(params, sql.as.int(start_time))
    end

    if end_time then
        time_condition = time_condition .. " AND timestamp <= ?"
        table.insert(params, sql.as.int(end_time))
    end

    local query = [[
        SELECT
            user_id,
            SUM(prompt_tokens) as prompt_tokens,
            SUM(completion_tokens) as completion_tokens,
            SUM(thinking_tokens) as thinking_tokens,
            SUM(total_tokens) as total_tokens,
            COUNT(DISTINCT usage_id) as request_count,
            COUNT(DISTINCT context_id) as context_count
        FROM token_usage
        WHERE user_id = ?]] .. time_condition .. [[
        GROUP BY user_id
    ]]

    local result, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to get user token totals: " .. err
    end

    -- Return zeros if no records found
    if #result == 0 then
        return {
            user_id = user_id,
            prompt_tokens = 0,
            completion_tokens = 0,
            thinking_tokens = 0,
            total_tokens = 0,
            request_count = 0,
            context_count = 0
        }
    end

    return result[1]
end

-- Get daily token usage for a period
function token_usage_repo.get_daily_usage(start_date, end_date)
    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = {}
    local date_condition = ""

    -- Add date filters if provided
    if start_date then
        date_condition = date_condition .. " WHERE date(timestamp, 'unixepoch') >= date(?, 'unixepoch')"
        table.insert(params, sql.as.int(start_date))
    end

    if end_date then
        if #date_condition == 0 then
            date_condition = " WHERE "
        else
            date_condition = date_condition .. " AND "
        end
        date_condition = date_condition .. "date(timestamp, 'unixepoch') <= date(?, 'unixepoch')"
        table.insert(params, sql.as.int(end_date))
    end

    local query = [[
        SELECT
            date(timestamp, 'unixepoch') as usage_date,
            SUM(prompt_tokens) as prompt_tokens,
            SUM(completion_tokens) as completion_tokens,
            SUM(thinking_tokens) as thinking_tokens,
            SUM(total_tokens) as total_tokens,
            COUNT(*) as request_count
        FROM token_usage]] .. date_condition .. [[
        GROUP BY date(timestamp, 'unixepoch')
        ORDER BY date(timestamp, 'unixepoch') ASC
    ]]

    local result, err

    -- Only pass params if we have any parameters
    if #params > 0 then
        result, err = db:query(query, params)
    else
        result, err = db:query(query)
    end

    db:release()

    if err then
        return nil, "Failed to get daily token usage: " .. err
    end

    return result
end

return token_usage_repo
