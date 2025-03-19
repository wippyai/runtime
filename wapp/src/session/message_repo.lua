local sql = require("sql")
local json = require("json")

-- Hardcoded database resource name
local DB_RESOURCE = "app:db"

local message_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Create a new message
function message_repo.create(message_id, session_id, msg_type, data, metadata)
    if not message_id or message_id == "" then
        return nil, "Message ID is required"
    end

    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not msg_type or msg_type == "" then
        return nil, "Message type is required"
    end

    if not data then
        return nil, "Message data is required"
    end

    -- Convert metadata to JSON if it's a table
    local metadata_json = nil
    if metadata then
        if type(metadata) == "table" then
            local encoded, err = json.encode(metadata)
            if err then
                return nil, "Failed to encode metadata: " .. err
            end
            metadata_json = encoded
        else
            metadata_json = metadata
        end
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Begin transaction
    local tx, err = db:begin()
    if err then
        db:release()
        return nil, "Failed to begin transaction: " .. err
    end

    local now = os.time()

    -- Insert the message - ensure metadata_json is never nil for SQL query
    -- If metadata is nil, we'll pass an empty string
    local params = {
        message_id,
        session_id,
        sql.as.int(now),
        msg_type,
        data,
        metadata_json or "" -- Use empty string if metadata_json is nil
    }

    local result, err = tx:execute(
        "INSERT INTO messages (message_id, session_id, date, type, data, metadata) VALUES (?, ?, ?, ?, ?, ?)",
        params
    )

    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to create message: " .. err
    end

    -- Update session's last message date
    result, err = tx:execute(
        "UPDATE sessions SET last_message_date = ? WHERE session_id = ?",
        { sql.as.int(now), session_id }
    )

    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to update session last message date: " .. err
    end

    -- Check if session was found
    if result.rows_affected == 0 then
        tx:rollback()
        db:release()
        return nil, "Session not found"
    end

    -- Commit transaction
    local success, err = tx:commit()
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to commit transaction: " .. err
    end

    db:release()

    return {
        message_id = message_id,
        session_id = session_id,
        date = now,
        type = msg_type
    }
end

-- Get a message by ID
function message_repo.get(message_id)
    if not message_id or message_id == "" then
        return nil, "Message ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT message_id, session_id, date, type, data, metadata
        FROM messages
        WHERE message_id = ?
    ]]

    local messages, err = db:query(query, { message_id })
    db:release()

    if err then
        return nil, "Failed to get message: " .. err
    end

    if #messages == 0 then
        return nil, "Message not found"
    end

    local message = messages[1]

    -- Parse metadata JSON if it exists
    if message.metadata and message.metadata ~= "" then
        local decoded, err = json.decode(message.metadata)
        if not err then
            message.metadata = decoded
        end
    end

    return message
end

-- List messages by session ID
function message_repo.list_by_session(session_id, limit, offset)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { session_id }
    local query = [[
        SELECT message_id, session_id, date, type, data, metadata
        FROM messages
        WHERE session_id = ?
        ORDER BY date ASC
    ]]

    -- Add limit and offset if provided
    if limit and limit > 0 then
        query = query .. " LIMIT ?"
        table.insert(params, limit)
        if offset and offset > 0 then
            query = query .. " OFFSET ?"
            table.insert(params, offset)
        end
    end

    local messages, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to list messages: " .. err
    end

    -- Parse metadata JSON if it exists
    for i, message in ipairs(messages) do
        if message.metadata and message.metadata ~= "" then
            local decoded, err = json.decode(message.metadata)
            if not err then
                message.metadata = decoded
            end
        end
    end

    return messages
end

-- List messages by type within a session
function message_repo.list_by_type(session_id, msg_type, limit, offset)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not msg_type or msg_type == "" then
        return nil, "Message type is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { session_id, msg_type }
    local query = [[
        SELECT message_id, session_id, date, type, data, metadata
        FROM messages
        WHERE session_id = ? AND type = ?
        ORDER BY date ASC
    ]]

    -- Add limit and offset if provided
    if limit and limit > 0 then
        query = query .. " LIMIT ?"
        table.insert(params, limit)
        if offset and offset > 0 then
            query = query .. " OFFSET ?"
            table.insert(params, offset)
        end
    end

    local messages, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to list messages by type: " .. err
    end

    -- Parse metadata JSON if it exists
    for i, message in ipairs(messages) do
        if message.metadata and message.metadata ~= "" then
            local decoded, err = json.decode(message.metadata)
            if not err then
                message.metadata = decoded
            end
        end
    end

    return messages
end

-- Get the latest message in a session
function message_repo.get_latest(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT message_id, session_id, date, type, data, metadata
        FROM messages
        WHERE session_id = ?
        ORDER BY date DESC
        LIMIT 1
    ]]

    local messages, err = db:query(query, { session_id })
    db:release()

    if err then
        return nil, "Failed to get latest message: " .. err
    end

    if #messages == 0 then
        return nil, "No messages found for this session"
    end

    local message = messages[1]

    -- Parse metadata JSON if it exists
    if message.metadata and message.metadata ~= "" then
        local decoded, err = json.decode(message.metadata)
        if not err then
            message.metadata = decoded
        end
    end

    return message
end

-- Delete a message
function message_repo.delete(message_id)
    if not message_id or message_id == "" then
        return nil, "Message ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if message exists
    local messages, err = db:query("SELECT message_id FROM messages WHERE message_id = ?", { message_id })
    if err then
        db:release()
        return nil, "Failed to check if message exists: " .. err
    end

    if #messages == 0 then
        db:release()
        return nil, "Message not found"
    end

    -- Delete the message
    local result, err = db:execute("DELETE FROM messages WHERE message_id = ?", { message_id })

    db:release()

    if err then
        return nil, "Failed to delete message: " .. err
    end

    return { deleted = true }
end

-- Count messages in a session
function message_repo.count_by_session(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT COUNT(*) as count
        FROM messages
        WHERE session_id = ?
    ]]

    local result, err = db:query(query, { session_id })
    db:release()

    if err then
        return nil, "Failed to count messages: " .. err
    end

    return result[1].count
end

-- Count messages by type in a session
function message_repo.count_by_type(session_id, msg_type)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not msg_type or msg_type == "" then
        return nil, "Message type is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT COUNT(*) as count
        FROM messages
        WHERE session_id = ? AND type = ?
    ]]

    local result, err = db:query(query, { session_id, msg_type })
    db:release()

    if err then
        return nil, "Failed to count messages by type: " .. err
    end

    return result[1].count
end

return message_repo