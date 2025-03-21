local prompt = require("prompt")
local json = require("json")
local tool_caller = require("tool_caller")

-- PromptBuilder component
local prompt_builder = {}
prompt_builder.__index = prompt_builder

-- Constructor
function prompt_builder.new(session_state)
    local self = setmetatable({}, prompt_builder)

    -- Store dependencies
    self.state = session_state

    return self
end

-- Build a prompt from conversation history
function prompt_builder:build_prompt(message_limit)
    -- Default to 50 messages if not specified
    message_limit = message_limit or 50

    -- Load messages from state
    local messages, err = self.state:load_messages(message_limit)
    if err then
        return nil, "Failed to load messages: " .. err
    end

    -- Sort by date (oldest first)
    if messages then
        table.sort(messages, function(a, b) return a.date < b.date end)
    else
        messages = {}
    end

    -- Create a prompt builder
    local builder = prompt.new()

    -- Process messages to add to prompt
    for i, msg in ipairs(messages) do
        local meta = msg.metadata or {}

        -- Special handling for delegation messages
        if msg.type == "delegation" then
            -- Convert delegation to tool call and result for LLM's benefit
            if meta.from_agent and meta.to_agent then
                -- Stable tool name based on target agent
                local delegate_tool_name = "delegate_to_" .. meta.to_agent

                -- Create arguments from delegation metadata
                local delegate_args = {
                    from = meta.from_agent,
                    message = meta.message or "Continuing with specialized agent"
                }

                -- Use delegation message ID as the function call ID
                local function_call_id = msg.message_id

                -- Add tool call representing the delegation action
                builder:add_function_call(
                    delegate_tool_name,
                    delegate_args,
                    function_call_id
                )

                -- Create result content
                local result_content = {
                    status = "accepted",
                    message = "Delegation accepted by " .. meta.to_agent
                }

                -- Add tool result representing acceptance
                builder:add_function_result(
                    delegate_tool_name,
                    json.encode(result_content),
                    function_call_id
                )
            end
        else
            -- Normal handling for other message types
            if msg.type == "system" then
                builder:add_system(msg.data)
            elseif msg.type == "user" then
                builder:add_user(msg.data)
            elseif msg.type == "assistant" then
                builder:add_assistant(msg.data)
            elseif msg.type == "developer" then
                builder:add_developer(msg.data)
            elseif msg.type == "function" then
                -- For function messages that contain both call and result
                if meta.function_name and meta.status then
                    if meta.status == tool_caller.FUNC_STATUS.PENDING then
                        -- This is a function call
                        local args = msg.data
                        -- Try to parse JSON if it's a string
                        if type(args) == "string" then
                            local parsed, parse_err = json.decode(args)
                            if not parse_err then
                                args = parsed
                            end
                        end

                        builder:add_function_call(
                            meta.function_name,
                            args,
                            meta.function_call_id
                        )
                    elseif meta.status == tool_caller.FUNC_STATUS.SUCCESS or
                        meta.status == tool_caller.FUNC_STATUS.ERROR then
                        -- This is a function result
                        builder:add_function_result(
                            meta.function_name,
                            msg.data,
                            meta.function_call_id
                        )
                    end
                end
            end
        end
    end

    return builder
end

return prompt_builder