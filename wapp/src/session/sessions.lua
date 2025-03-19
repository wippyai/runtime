-- High-level API for the app.sessions namespace
local context_repo = require("context_repo")
local session_repo = require("session_repo")
local message_repo = require("message_repo")
local token_usage_repo = require("token_usage_repo")
local uuid = require("uuid")

local sessions = {}

--------------------------------------------------------------------------------
-- Context Operations
--------------------------------------------------------------------------------

-- Create a new context
function sessions.create_context(type, data)
    local context_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate UUID: " .. err
    end
    return context_repo.create(context_id, type, data)
end

-- Get a context by ID
function sessions.get_context(context_id)
    return context_repo.get(context_id)
end

-- Update context data
function sessions.update_context(context_id, data)
    return context_repo.update(context_id, data)
end

-- Get contexts by type
function sessions.get_contexts_by_type(type, limit, offset)
    return context_repo.get_by_type(type, limit, offset)
end

--------------------------------------------------------------------------------
-- Session Operations
--------------------------------------------------------------------------------

-- Create a new session with a primary data context
function sessions.create_session(user_id, initial_data, title, kind, current_model, current_agent)
    -- Create primary data context
    local context_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate context UUID: " .. err
    end

    local context, err = context_repo.create(context_id, "data", initial_data)
    if err then
        return nil, "Failed to create primary context: " .. err
    end

    -- Create session
    local session_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate session UUID: " .. err
    end

    return session_repo.create(session_id, user_id, context_id, title, kind, current_model, current_agent)
end

-- Get a session by ID
function sessions.get_session(session_id)
    return session_repo.get(session_id)
end

-- Get sessions for a user
function sessions.list_user_sessions(user_id, limit, offset)
    return session_repo.list_by_user(user_id, limit, offset)
end

-- Update session title
function sessions.update_session_title(session_id, title)
    return session_repo.update_title(session_id, title)
end

-- Update session metadata (model, agent, and/or title) in a single transaction
function sessions.update_session_metadata(session_id, updates)
    if not updates or type(updates) ~= "table" then
        return nil, "Updates must be a table containing fields to update"
    end

    -- Create a valid updates object for the repo function
    local repo_updates = {}

    -- Copy only valid fields to update
    if updates.title ~= nil then
        repo_updates.title = updates.title
    end

    if updates.current_model ~= nil then
        repo_updates.current_model = updates.current_model
    end

    if updates.current_agent ~= nil then
        repo_updates.current_agent = updates.current_agent
    end

    -- Always update last_message_date when metadata changes
    repo_updates.last_message_date = os.time()

    return session_repo.update_session_meta(session_id, repo_updates)
end

-- Add an additional context to a session
function sessions.add_session_context(session_id, type, data)
    -- Create the context
    local context_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate UUID: " .. err
    end

    local context, err = context_repo.create(context_id, type, data)
    if err then
        return nil, "Failed to create context: " .. err
    end

    -- Link it to the session
    return session_repo.add_context(session_id, context_id)
end

-- Get all contexts for a session
function sessions.get_session_contexts(session_id)
    return session_repo.get_contexts(session_id)
end

-- Delete a session and all its data
function sessions.delete_session(session_id)
    return session_repo.delete(session_id)
end

--------------------------------------------------------------------------------
-- Message Operations
--------------------------------------------------------------------------------

-- Add a message to a session
function sessions.add_message(session_id, type, data, metadata)
    local message_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate UUID: " .. err
    end
    return message_repo.create(message_id, session_id, type, data, metadata)
end

-- Get a message by ID
function sessions.get_message(message_id)
    return message_repo.get(message_id)
end

-- Get messages for a session
function sessions.get_session_messages(session_id, limit, offset)
    return message_repo.list_by_session(session_id, limit, offset)
end

-- Get messages of a specific type
function sessions.get_messages_by_type(session_id, type, limit, offset)
    return message_repo.list_by_type(session_id, type, limit, offset)
end

-- Get the most recent message in a session
function sessions.get_latest_message(session_id)
    return message_repo.get_latest(session_id)
end

-- Count messages in a session
function sessions.count_session_messages(session_id)
    return message_repo.count_by_session(session_id)
end

return sessions
