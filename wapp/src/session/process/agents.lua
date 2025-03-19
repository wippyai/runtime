local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local json = require("json")

-- Agent Manager
-- Handles agent initialization, message processing and delegation
local agent_manager = {}

-- Create a new agent instance
function agent_manager.create(agent_id, model)
    if not agent_id then
        return nil, "Agent ID is required"
    end

    -- Get agent specification from registry
    local agent_spec, err = agent_registry.get_by_id(agent_id)
    if not agent_spec then
        return nil, "Failed to load agent specification: " .. (err or "unknown error")
    end

    -- Override the model if provided
    if model then
        agent_spec.model = model
    end

    -- Create agent runner
    local agent, err = agent_runner.new(agent_spec)
    if not agent then
        return nil, "Failed to create agent: " .. (err or "unknown error")
    end

    return {
        agent = agent,
        spec = agent_spec
    }
end

-- Load conversation history into an agent
function agent_manager.load_history(agent, messages)
    if not agent or not messages then
        return false, "Agent and messages are required"
    end

    -- Process messages and add to agent in order
    for _, message in ipairs(messages) do
        if message.type == "user" then
            -- Add user message
            local data = message.data
            if type(data) == "string" then
                agent:add_user_message(data)
            elseif type(data) == "table" and data.data then
                agent:add_user_message(data.data)
            end
        elseif message.type == "assistant" then
            -- Add assistant message
            local data = message.data
            if type(data) == "string" then
                agent:add_assistant_message(data)
            elseif type(data) == "table" and data.data then
                agent:add_assistant_message(data.data)
            end
        elseif message.type == "tool_call" then
            -- Add tool call
            local data = message.data
            local metadata = message.metadata

            -- Parse JSON if stored as string
            if type(data) == "string" and data:match("^%s*{") then
                data = json.decode(data)
            end

            if type(metadata) == "string" and metadata:match("^%s*{") then
                metadata = json.decode(metadata)
            end

            if data and metadata and metadata.call_id then
                agent:add_function_call(
                    metadata.tool_id or data.tool_name or "unknown",
                    metadata.arguments or {},
                    metadata.call_id
                )
            end
        elseif message.type == "tool_result" then
            -- Add tool result
            local data = message.data
            local metadata = message.metadata

            -- Parse JSON if stored as string
            if type(data) == "string" and data:match("^%s*{") then
                data = json.decode(data)
            end

            if type(metadata) == "string" and metadata:match("^%s*{") then
                metadata = json.decode(metadata)
            end

            if data and data.parent_call_id then
                agent:add_function_result(
                    data.tool_name or "unknown",
                    metadata and metadata.result or {},
                    data.parent_call_id
                )
            end
        end
        -- Ignore other message types for now
    end

    return true
end
