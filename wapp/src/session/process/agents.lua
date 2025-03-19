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

-- Process a message using the agent runner
function agent_manager.process_message(agent, opts)
    if not agent then
        return nil, "Agent is required"
    end

    -- Default options
    opts = opts or {}

    -- Execute the agent step
    local result, err = agent.agent:step()
    if err then
        return nil, "Error executing agent: " .. err
    end

    return result
end

-- Handle delegation from one agent to another
function agent_manager.handle_delegation(result, old_agent_id, old_model)
    if not result or not result.delegate_target then
        return nil, "Not a delegation result"
    end

    -- Create new agent instance
    local new_agent_data, err = agent_manager.create(result.delegate_target, old_model)
    if not new_agent_data then
        return nil, "Failed to create delegated agent: " .. (err or "unknown error")
    end

    -- Add the delegation message to the new agent
    new_agent_data.agent:add_user_message(result.delegate_message)

    -- Get new agent ID
    local new_agent_id = result.delegate_target

    -- Return delegation info
    return {
        from_agent = old_agent_id,
        to_agent = new_agent_id,
        new_agent = new_agent_data.agent,
        new_spec = new_agent_data.spec,
        delegate_message = result.delegate_message
    }
end

-- Register tools with an agent
function agent_manager.register_tools(agent, tools)
    if not agent or not agent.agent then
        return false, "Agent is required"
    end

    if not tools or type(tools) ~= "table" then
        return true -- No tools to register
    end

    -- Register each tool
    for name, schema in pairs(tools) do
        agent.agent:register_tool(name, schema)
    end

    return true
end

-- Get agent statistics
function agent_manager.get_stats(agent)
    if not agent or not agent.agent then
        return nil, "Agent is required"
    end

    return agent.agent:get_stats()
end

-- Add a user message to the agent
function agent_manager.add_user_message(agent, message)
    if not agent or not agent.agent then
        return false, "Agent is required"
    end

    if not message then
        return false, "Message is required"
    end

    agent.agent:add_user_message(message)
    return true
end

-- Add an assistant message to the agent
function agent_manager.add_assistant_message(agent, message)
    if not agent or not agent.agent then
        return false, "Agent is required"
    end

    if not message then
        return false, "Message is required"
    end

    agent.agent:add_assistant_message(message)
    return true
end

-- Add a function call to the agent
function agent_manager.add_function_call(agent, name, arguments, call_id)
    if not agent or not agent.agent then
        return false, "Agent is required"
    end

    if not name or not call_id then
        return false, "Function name and call ID are required"
    end

    agent.agent:add_function_call(name, arguments, call_id)
    return true
end

-- Add a function result to the agent
function agent_manager.add_function_result(agent, name, result, call_id)
    if not agent or not agent.agent then
        return false, "Agent is required"
    end

    if not name or not call_id then
        return false, "Function name and call ID are required"
    end

    agent.agent:add_function_result(name, result, call_id)
    return true
end

-- Clear agent history
function agent_manager.clear_history(agent)
    if not agent or not agent.agent then
        return false, "Agent is required"
    end

    agent.agent:clear_history()
    return true
end

return agent_manager
