local sql = require("sql")

-- Hardcoded database resource name
local DB_RESOURCE = "app:db"

local session_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Create a new session
function session_repo.create(session_id, user_id, primary_context_id, title, kind, current_model, current_agent)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    if not primary_context_id or primary_context_id == "" then
        return nil, "Primary context ID is required"
    end

    -- Default values for optional parameters
    title = title or ""
    kind = kind or "default"
    current_model = current_model or ""
    current_agent = current_agent or ""

    local db, err = get_db()
    if err then
        return nil, err
    end

    local now = os.time()

    local result, err = db:execute(
        [[INSERT INTO sessions
          (session_id, user_id, primary_context_id, title, kind, current_model, current_agent, start_date, last_message_date)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)]],
        { session_id, user_id, primary_context_id, title, kind, current_model, current_agent, sql.as.int(now), sql.as
            .int(now) }
    )

    db:release()

    if err then
        return nil, "Failed to create session: " .. err
    end

    return {
        session_id = session_id,
        user_id = user_id,
        primary_context_id = primary_context_id,
        title = title,
        kind = kind,
        current_model = current_model,
        current_agent = current_agent,
        start_date = now,
        last_message_date = now
    }
end

-- Get a session by ID
function session_repo.get(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT session_id, user_id, primary_context_id, title, kind, current_model, current_agent, start_date, last_message_date
        FROM sessions
        WHERE session_id = ?
    ]]

    local sessions, err = db:query(query, { session_id })
    db:release()

    if err then
        return nil, "Failed to get session: " .. err
    end

    if #sessions == 0 then
        return nil, "Session not found"
    end

    return sessions[1]
end

-- List sessions by user ID
function session_repo.list_by_user(user_id, limit, offset)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local params = { user_id }
    local query = [[
        SELECT session_id, user_id, status, primary_context_id, title, kind, current_model, current_agent, start_date, last_message_date
        FROM sessions
        WHERE user_id = ?
        ORDER BY last_message_date DESC
    ]]

    -- Add limit and offset if provided
    if limit and limit > 0 then
        query = query .. " LIMIT ?"
        table.insert(params, sql.as.int(limit))

        if offset and offset > 0 then
            query = query .. " OFFSET ?"
            table.insert(params, sql.as.int(offset))
        end
    end

    local sessions, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to list sessions: " .. err
    end

    return sessions
end

-- Update session title
function session_repo.update_title(session_id, title)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not title then
        title = "" -- Default to empty string if title is nil
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if session exists
    local sessions, err = db:query("SELECT session_id FROM sessions WHERE session_id = ?", { session_id })
    if err then
        db:release()
        return nil, "Failed to check if session exists: " .. err
    end

    if #sessions == 0 then
        db:release()
        return nil, "Session not found"
    end

    -- Update session title
    local result, err = db:execute(
        "UPDATE sessions SET title = ? WHERE session_id = ?",
        { title, session_id }
    )

    db:release()

    if err then
        return nil, "Failed to update session title: " .. err
    end

    return {
        session_id = session_id,
        title = title,
        updated = true
    }
end

-- Update last message date
function session_repo.update_last_message_date(session_id, date)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    -- Default to current time if date not provided
    date = date or os.time()

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if session exists
    local sessions, err = db:query("SELECT session_id FROM sessions WHERE session_id = ?", { session_id })
    if err then
        db:release()
        return nil, "Failed to check if session exists: " .. err
    end

    if #sessions == 0 then
        db:release()
        return nil, "Session not found"
    end

    -- Update last message date
    local result, err = db:execute(
        "UPDATE sessions SET last_message_date = ? WHERE session_id = ?",
        { sql.as.int(date), session_id }
    )

    db:release()

    if err then
        return nil, "Failed to update last message date: " .. err
    end

    return {
        session_id = session_id,
        last_message_date = date,
        updated = true
    }
end

-- Update session metadata (model, agent, and last_message_date) in a single transaction
function session_repo.update_session_meta(session_id, updates)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not updates or type(updates) ~= "table" then
        return nil, "Updates table is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if session exists
    local sessions, err = db:query("SELECT session_id FROM sessions WHERE session_id = ?", { session_id })
    if err then
        db:release()
        return nil, "Failed to check if session exists: " .. err
    end

    if #sessions == 0 then
        db:release()
        return nil, "Session not found"
    end

    -- Build update query based on provided fields
    local set_clauses = {}
    local params = {}
    local result = { session_id = session_id, updated = true }

    -- Add fields to update
    if updates.current_model ~= nil then
        table.insert(set_clauses, "current_model = ?")
        table.insert(params, updates.current_model)
        result.current_model = updates.current_model
    end

    if updates.current_agent ~= nil then
        table.insert(set_clauses, "current_agent = ?")
        table.insert(params, updates.current_agent)
        result.current_agent = updates.current_agent
    end

    if updates.title ~= nil then
        table.insert(set_clauses, "title = ?")
        table.insert(params, updates.title)
        result.title = updates.title
    end

    -- Always update last_message_date if requested or if any other field is updated
    if updates.last_message_date ~= nil or #set_clauses > 0 then
        local date = updates.last_message_date or os.time()
        table.insert(set_clauses, "last_message_date = ?")
        table.insert(params, sql.as.int(date))
        result.last_message_date = date
    end

    -- If nothing to update, return early
    if #set_clauses == 0 then
        db:release()
        return result
    end

    -- Add session_id to params
    table.insert(params, session_id)

    -- Execute update query
    local update_query = "UPDATE sessions SET " .. table.concat(set_clauses, ", ") .. " WHERE session_id = ?"
    local update_result, err = db:execute(update_query, params)

    db:release()

    if err then
        return nil, "Failed to update session metadata: " .. err
    end

    return result
end

-- Add a context to a session (many-to-many relationship)
function session_repo.add_context(session_id, context_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if session exists
    local sessions, err = db:query("SELECT session_id FROM sessions WHERE session_id = ?", { session_id })
    if err then
        db:release()
        return nil, "Failed to check if session exists: " .. err
    end

    if #sessions == 0 then
        db:release()
        return nil, "Session not found"
    end

    -- Check if context exists
    local contexts, err = db:query("SELECT context_id FROM contexts WHERE context_id = ?", { context_id })
    if err then
        db:release()
        return nil, "Failed to check if context exists: " .. err
    end

    if #contexts == 0 then
        db:release()
        return nil, "Context not found"
    end

    -- Check if relationship already exists
    local existing, err = db:query(
        "SELECT session_id, context_id FROM session_contexts WHERE session_id = ? AND context_id = ?",
        { session_id, context_id }
    )
    if err then
        db:release()
        return nil, "Failed to check existing relationship: " .. err
    end

    if #existing > 0 then
        db:release()
        return {
            session_id = session_id,
            context_id = context_id,
            added = false,
            message = "Relationship already exists"
        }
    end

    -- Add the relationship
    local result, err = db:execute(
        "INSERT INTO session_contexts (session_id, context_id) VALUES (?, ?)",
        { session_id, context_id }
    )

    db:release()

    if err then
        return nil, "Failed to add context to session: " .. err
    end

    return {
        session_id = session_id,
        context_id = context_id,
        added = true
    }
end

-- Remove a context from a session
function session_repo.remove_context(session_id, context_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Delete the relationship
    local result, err = db:execute(
        "DELETE FROM session_contexts WHERE session_id = ? AND context_id = ?",
        { session_id, context_id }
    )

    db:release()

    if err then
        return nil, "Failed to remove context from session: " .. err
    end

    -- Check if any rows were affected
    if result.rows_affected == 0 then
        return {
            session_id = session_id,
            context_id = context_id,
            removed = false,
            message = "Relationship did not exist"
        }
    end

    return {
        session_id = session_id,
        context_id = context_id,
        removed = true
    }
end

-- Get all contexts for a session
function session_repo.get_contexts(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT c.context_id, c.type, c.data
        FROM contexts c
        JOIN session_contexts sc ON c.context_id = sc.context_id
        WHERE sc.session_id = ?
    ]]

    local contexts, err = db:query(query, { session_id })
    db:release()

    if err then
        return nil, "Failed to get contexts for session: " .. err
    end

    return contexts
end

-- Delete a session and all its relationships
function session_repo.delete(session_id)
    if not session_id or session_id == "" then
        return nil, "Session ID is required"
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

    -- Delete session contexts relationships
    local result, err = tx:execute("DELETE FROM session_contexts WHERE session_id = ?", { session_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete session contexts: " .. err
    end

    -- Delete messages
    result, err = tx:execute("DELETE FROM messages WHERE session_id = ?", { session_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete session messages: " .. err
    end

    -- Delete the session
    result, err = tx:execute("DELETE FROM sessions WHERE session_id = ?", { session_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete session: " .. err
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

    return { deleted = true }
end

return session_repo
