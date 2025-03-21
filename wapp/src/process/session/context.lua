local json = require("json")
local context_repo = require("context_repo")

-- Command action constants
local ACTIONS = {
    WRITE = "write",
    DELETE = "delete",
    READ = "read"
}

-- Command response types constants
local COMMANDS = {
    COMMAND_SUCCESS = "command_success"
}

-- Error constants
local ERR = {
    CONTEXT_KEY_REQUIRED = "Context key is required",
    INVALID_ACTION = "Invalid context action",
    ACTION_REQUIRED = "Context action is required",
    CONTEXT_GET_FAILED = "Failed to get context",
    CONTEXT_UPDATE_FAILED = "Failed to update context",
    CONTEXT_ID_REQUIRED = "Context ID is required"
}

-- SessionContext class
local session_context = {}
session_context.__index = session_context

-- Constructor
function session_context.new(context_id)
    local self = setmetatable({}, session_context)

    -- Store dependencies
    self.context_id = context_id

    return self
end

-- Main command handler for context operations
function session_context:handle_command(payload)
    if not payload.action then
        return false, ERR.ACTION_REQUIRED
    end

    if payload.action == ACTIONS.WRITE then
        return self:write_context(payload.key, payload.data, payload.from_pid, payload.request_id)
    elseif payload.action == ACTIONS.DELETE then
        return self:delete_context(payload.key, payload.from_pid, payload.request_id)
    elseif payload.action == ACTIONS.READ then
        return self:read_context(payload.key, payload.from_pid, payload.request_id)
    else
        return false, ERR.INVALID_ACTION
    end
end

-- Get the full context data
function session_context:get_full_context()
    if not self.context_id then
        return nil, ERR.CONTEXT_ID_REQUIRED
    end

    -- Get the current context data
    local context, err = context_repo.get(self.context_id)
    if err then
        return nil, ERR.CONTEXT_GET_FAILED .. ": " .. err
    end

    -- Parse context data
    local context_data = {}
    if context and context.data then
        local decoded, parse_err = json.decode(context.data)
        if not parse_err then
            context_data = decoded
        end
    end

    return context_data
end

-- Write to context
function session_context:write_context(key, data, from_pid, request_id)
    if not key then
        return false, ERR.CONTEXT_KEY_REQUIRED
    end

    if not self.context_id then
        return false, ERR.CONTEXT_ID_REQUIRED
    end

    -- Get the current context data
    local context_data, err = self:get_full_context()
    if err then
        return false, err
    end

    -- Update the context data
    context_data[key] = data

    -- Save updated context
    local success, update_err = context_repo.update(self.context_id, json.encode(context_data))
    if not success then
        return false, ERR.CONTEXT_UPDATE_FAILED .. ": " .. update_err
    end

    -- Send direct message if from_pid provided
    if from_pid and process and process.send then
        process.send(from_pid, COMMANDS.COMMAND_SUCCESS, {
            action = ACTIONS.WRITE,
            key = key,
            context_id = self.context_id,
            context = context_data
        })
    end

    return true
end

-- Delete from context
function session_context:delete_context(key, from_pid, request_id)
    if not key then
        return false, ERR.CONTEXT_KEY_REQUIRED
    end

    if not self.context_id then
        return false, ERR.CONTEXT_ID_REQUIRED
    end

    -- Get the current context data
    local context_data, err = self:get_full_context()
    if err then
        return false, err
    end

    -- Remove the key
    context_data[key] = nil

    -- Save updated context
    local success, update_err = context_repo.update(self.context_id, json.encode(context_data))
    if not success then
        return false, ERR.CONTEXT_UPDATE_FAILED .. ": " .. update_err
    end

    -- Send direct message if from_pid provided
    if from_pid and process and process.send then
        process.send(from_pid, COMMANDS.COMMAND_SUCCESS, {
            action = ACTIONS.DELETE,
            key = key,
            context_id = self.context_id,
            context = context_data
        })
    end

    return true
end

-- Read from context
function session_context:read_context(key, from_pid, request_id)
    if not key then
        return false, ERR.CONTEXT_KEY_REQUIRED
    end

    if not self.context_id then
        return false, ERR.CONTEXT_ID_REQUIRED
    end

    -- Get the current context data
    local context_data, err = self:get_full_context()
    if err then
        return false, err
    end

    -- Get the value
    local value = context_data[key]

    -- Send direct message if from_pid provided
    if from_pid and process and process.send then
        process.send(from_pid, COMMANDS.COMMAND_SUCCESS, {
            action = ACTIONS.READ,
            key = key,
            value = value,
            context_id = self.context_id,
            context = context_data
        })
    end

    return true, value
end

-- Export constants
session_context.ACTIONS = ACTIONS
session_context.COMMANDS = COMMANDS
session_context.ERR = ERR

return session_context