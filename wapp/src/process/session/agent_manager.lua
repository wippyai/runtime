local json = require("json")
local uuid = require("uuid")
local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")

-- Error constants
local ERR = {
    NO_AGENT = "No agent configured for this session",
    AGENT_LOAD_FAILED = "Failed to load agent",
    AGENT_NAME_REQUIRED = "Agent name is required",
    MODEL_NAME_REQUIRED = "Model name is required",
    DELEGATION_FAILED = "Failed to delegate to agent"
}

-- AgentManager class
local agent_manager = {}
agent_manager.__index = agent_manager

-- Constructor
function agent_manager.new(session_state, upstream)
    local self = setmetatable({}, agent_manager)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream

    -- Agent instance cache
    self.agent = nil

    return self
end

-- Change to a different agent
function agent_manager:change_agent(agent_name, reason)
    if not agent_name then
        return nil, ERR.AGENT_NAME_REQUIRED
    end

    -- Get agent spec by name to validate it exists
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        return nil, ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
    end

    -- Remember current agent for logging
    local previous_agent = self.state.agent_name

    -- Reset agent instance
    self.agent = nil

    -- Update agent name in state
    local success, err = self.state:set_agent_config(agent_name, self.state.model)
    if not success then
        return nil, err
    end

    -- Get the model from agent spec if it has one and current model is empty
    if agent_spec.model and (not self.state.model or self.state.model == "") then
        self:change_model(agent_spec.model, "Automatically selected for " .. agent_name)
    end

    -- Notify clients about agent change
    if self.upstream then
        self.upstream:update_session({
            agent = agent_name
        })
    end

    -- Log the change if previous agent was set
    if previous_agent then
        -- Create system message for agent change
        local metadata = {
            source = "system",
            agent_change = {
                from = previous_agent,
                to = agent_name
            },
            reason = reason
        }

        local message = "Agent changed from " .. previous_agent ..
                        " to " .. agent_name ..
                        (reason and (": " .. reason) or "")

        -- Add system message
        self.state:add_system_message(message, metadata)
    end

    return true
end

-- Change model
function agent_manager:change_model(model_name, reason)
    if not model_name then
        return nil, ERR.MODEL_NAME_REQUIRED
    end

    -- Remember current model for logging
    local previous_model = self.state.model

    -- Reset agent instance to force reload with new model
    self.agent = nil

    -- Update model in state
    local success, err = self.state:set_agent_config(self.state.agent_name, model_name)
    if not success then
        return nil, err
    end

    -- Notify clients about model change
    if self.upstream then
        self.upstream:update_session({
            model = model_name
        })
    end

    -- Log the change if previous model was set
    if previous_model and previous_model ~= "" then
        -- Create system message for model change
        local metadata = {
            source = "system",
            model_change = {
                from = previous_model,
                to = model_name
            },
            reason = reason
        }

        local message = "Model changed from " .. previous_model ..
                        " to " .. model_name ..
                        (reason and (": " .. reason) or "")

        -- Add system message
        self.state:add_system_message(message, metadata)
    end

    return true
end

-- Get current agent instance (lazy loading)
function agent_manager:get_agent()
    -- If we already have an agent, return it
    if self.agent then
        return self.agent
    end

    -- Get agent details from state
    local agent_name = self.state.agent_name
    local model = self.state.model

    if not agent_name then
        return nil, ERR.NO_AGENT
    end

    -- Get agent spec by name from the agent registry
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
        return nil, error_msg
    end

    -- Override the model if specified
    if model then
        agent_spec.model = model
    end

    -- Add tool IDs if available
    if self.state.tool_ids and #self.state.tool_ids > 0 then
        agent_spec.tools = self.state.tool_ids
    end

    -- Create new agent runner from spec
    local agent, err = agent_runner.new(agent_spec)
    if err then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. err
        return nil, error_msg
    end

    -- Store the agent
    self.agent = agent

    return self.agent
end

-- Generate a title using the agent
function agent_manager:generate_title(prompt_builder)
    -- Get an agent instance
    local agent, err = self:get_agent()
    if err then
        return nil, err
    end

    -- Generate a title using the agent's model
    local result, err = agent:generate_title(prompt_builder)
    if err then
        return nil, err
    end

    return result
end

-- Process delegation to another agent
function agent_manager:process_delegation(delegation_data, message_id, response_id)
    if not delegation_data.target_agent then
        return false, "No delegation target specified"
    end

    local first_agent_response = delegation_data.result or ""
    local has_response = first_agent_response and #first_agent_response > 0

    -- If the first agent provided a non-empty response, store it
    if has_response then
        -- Create metadata for the partial response
        local metadata = {
            agent_name = self.state.agent_name,
            model = self.state.model,
            is_delegate_partial = true,
            delegated_to = delegation_data.target_agent
        }

        -- Store partial response in state
        local stored_response_id, err = self.state:add_assistant_message(
            first_agent_response,
            metadata
        )

        if err then
            -- Don't fail the operation, just log the warning
            print("Warning: Failed to store first agent response before delegation: " .. err)
        else
            -- Send the partial content update
            self.upstream:send_message_update(response_id, "content", {
                content = first_agent_response,
                is_partial = true
            })
        end
    end

    -- Generate a new response ID for the delegated agent response
    local delegated_response_id, err = uuid.v7()
    if err then
        return false, "Failed to generate delegated response ID: " .. err
    end

    -- Create delegation record
    local delegation_metadata = {
        system_action = "delegation",
        from_agent = self.state.agent_name,
        to_agent = delegation_data.target_agent,
        reason = delegation_data.reason,
        message = delegation_data.message or "Continuing with specialized agent",
        original_response_id = response_id,
        delegated_response_id = delegated_response_id
    }

    -- Add a delegation record in state
    local delegation_id = self.state:add_message(
        "delegation",
        "Delegation from '" .. self.state.agent_name .. "' to '" .. delegation_data.target_agent .. "'",
        delegation_metadata
    )

    -- Switch agent
    local success, change_err = self:change_agent(
        delegation_data.target_agent,
        delegation_data.reason or "Delegation requested"
    )

    if not success then
        -- Log the error but continue with the response
        print("Failed to switch to delegate agent: " .. change_err)
        return false, "Failed to switch agent: " .. change_err
    end

    -- Create the continuation data
    local continuation_data = {
        type = "continue",
        message_id = message_id,
        response_id = delegated_response_id, -- Use new response_id
        original_response_id = response_id,  -- Keep track of original for reference
        delegation = {
            delegation_id = delegation_id,
            from_agent = delegation_metadata.from_agent,
            to_agent = delegation_data.target_agent,
            message = delegation_data.message,
            had_partial_response = has_response
        }
    }

    -- Announce the beginning of the delegated response
    self.upstream:response_beginning(message_id, delegated_response_id)

    return true, continuation_data
end

-- Export constants
agent_manager.ERR = ERR

return agent_manager