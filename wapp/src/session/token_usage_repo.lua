local sql = require("sql")

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
function token_usage_repo.record(usage_id, session_id, model_name, prompt_tokens, completion_tokens, thinking_tokens)
    if not usage_id or usage_id == "" then
        return nil, "Usage ID is required"
    end

    if not session_id or session_id == "" then
        return nil, "Session ID is required"
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

    local db, err = get_db()
    if err then
        return nil, err
    end

    local now = os.time()

    local result, err = db:execute(
        [[INSERT INTO token_usage
          (usage_id, session_id, model_name, prompt_tokens, completion_tokens, thinking_tokens, total_tokens, timestamp)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?)]],
        { usage_id, session_id, model_name, prompt_tokens, completion_tokens, thinking_tokens, total_tokens, now }
    )

    db:release()

    if err then
        return nil, "Failed to record token usage: " .. err
    end

    return {
        usage_id = usage_id,
        session_id = session_id,
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
        SELECT usage_id, session_id, model_name, prompt_tokens, completion_tokens,
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

-- Get all token usage for a session
function token_usage_repo.get_by_session(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT usage_id, session_id, model_name, prompt_tokens, completion_tokens,
               thinking_tokens, total_tokens, timestamp
        FROM token_usage
        WHERE session_id = ?
        ORDER BY timestamp ASC
    ]]

    local usages, err = db:query(query, { session_id })
    db:release()

    if err then
        return nil, "Failed to get token usage for session: " .. err
    end

    return usages
end

-- Get aggregated token usage for a session
function token_usage_repo.get_session_totals(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
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
        WHERE session_id = ?
    ]]

    local result, err = db:query(query, { session_id })
    db:release()

    if err then
        return nil, "Failed to get session token totals: " .. err
    end

    -- Return zeros if no records found
    if not result[1].prompt_tokens then
        return {
            session_id = session_id,
            prompt_tokens = 0,
            completion_tokens = 0,
            thinking_tokens = 0,
            total_tokens = 0,
            request_count = 0
        }
    end

    result[1].session_id = session_id
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
        table.insert(params, start_time)
    end

    if end_time then
        time_condition = time_condition .. " AND timestamp <= ?"
        table.insert(params, end_time)
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
        time_condition = time_condition .. " AND tu.timestamp >= ?"
        table.insert(params, start_time)
    end

    if end_time then
        time_condition = time_condition .. " AND tu.timestamp <= ?"
        table.insert(params, end_time)
    end

    local query = [[
        SELECT
            s.user_id,
            SUM(tu.prompt_tokens) as prompt_tokens,
            SUM(tu.completion_tokens) as completion_tokens,
            SUM(tu.thinking_tokens) as thinking_tokens,
            SUM(tu.total_tokens) as total_tokens,
            COUNT(DISTINCT tu.usage_id) as request_count,
            COUNT(DISTINCT tu.session_id) as session_count
        FROM token_usage tu
        JOIN sessions s ON tu.session_id = s.session_id
        WHERE s.user_id = ?]] .. time_condition .. [[
        GROUP BY s.user_id
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
            session_count = 0
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
        table.insert(params, start_date)
    end

    if end_date then
        if #date_condition == 0 then
            date_condition = " WHERE "
        else
            date_condition = date_condition .. " AND "
        end
        date_condition = date_condition .. "date(timestamp, 'unixepoch') <= date(?, 'unixepoch')"
        table.insert(params, end_date)
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

    local result, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to get daily token usage: " .. err
    end

    return result
end

return token_usage_repo