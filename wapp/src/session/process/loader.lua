-- wippy.session:loader
-- Session state loader for session processes

local json = require("json")
local uuid = require("uuid")

-- Repository imports
local context_repo = require("context_repo")
local session_repo = require("session_repo")
local message_repo = require("message_repo")
local start_tokens = require("start_tokens")

local loader = {}

-- Create a new session
function loader.create_session(args)
    -- Process start token (required for new sessions)
    local token_data, err = start_tokens.unpack(args.start_token)
    if err then
        return nil, "Invalid start token: " .. err
    end

    -- Session ID should always be provided by user process
    if not args.session_id then
        return nil, "Session ID is required"
    end

    -- Prepare context data (merge contexts, token takes precedence)
    local context_data = {}

    -- Add start context if provided
    if args.start_context then
        context_data = args.start_context
    end

    -- Override with token context if available
    if token_data.context then
        -- Merge token context over start context
        for k, v in pairs(token_data.context) do
            context_data[k] = v
        end
    end

    -- Create primary context
    local context_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate context UUID: " .. err
    end

    -- todo: wrap to tx or add cleanup
    local context, err = context_repo.create(context_id, "data", json.encode(context_data))
    if err then
        return nil, "Failed to create primary context: " .. err
    end

    -- Create session
    local session, err = session_repo.create(
        args.session_id,
        args.user_id,
        context_id,
        "",
        token_data.kind or "default",
        token_data.model,
        token_data.agent
    )

    if err then
        return nil, "Failed to create session: " .. err
    end

    -- Return state with session and context info
    return {
        session_id = args.session_id,
        user_id = args.user_id,
        primary_context_id = context_id,
        meta = {
            agent = token_data.agent,
            model = token_data.model,
            kind = token_data.kind or "default",
        },
        start_date = session.start_date,
        last_message_date = session.last_message_date,
        status = "idle"
    }
end

-- Load an existing session
function loader.load_session(args)
    -- Session ID should always be provided
    if not args.session_id then
        return nil, "Session ID is required"
    end

    local session, err = session_repo.get(args.session_id)
    if err then
        return nil, "Failed to load session: " .. err
    end

    -- Verify session belongs to this user
    if session.user_id ~= args.user_id then
        return nil, "Session belongs to a different user"
    end

    -- Get the latest message for continuity
    local latest_message, _ = message_repo.get_latest(args.session_id)
    local last_message_id = nil
    if latest_message then
        last_message_id = latest_message.message_id
    end

    -- Return state with session info
    return {
        session_id = args.session_id,
        user_id = args.user_id,
        primary_context_id = session.primary_context_id,
        meta = {
            agent = session.current_agent,
            model = session.current_model,
            kind = session.kind,
        },
        start_date = session.start_date,
        last_message_date = session.last_message_date,
        status = session.status or "idle",
        last_message_id = last_message_id
    }
end

return loader
