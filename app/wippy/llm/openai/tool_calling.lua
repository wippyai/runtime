local openai = require("openai_client")
local output = require("output")
local tools = require("tools")

-- Function Calling Handler
-- Supports text generation with tool/function calling capabilities
local function handler(args)
    -- Validate required arguments
    if not args.model then
        return {
            error = output.ERROR_TYPE.INVALID_REQUEST,
            error_message = "Model is required"
        }
    end

    -- Format messages from various input formats
    local messages = args.messages or {}

    if #messages == 0 then
        return {
            error = output.ERROR_TYPE.INVALID_REQUEST,
            error_message = "No messages provided"
        }
    }

    -- Configure options objects for easier management
    local options = args.options or {}

    -- Configure request payload
    local payload = {
        model = args.model,
        messages = messages,
        temperature = options.temperature,
        top_p = options.top_p,
        top_k = options.top_k,
        max_tokens = options.max_tokens,
        presence_penalty = options.presence_penalty,
        frequency_penalty = options.frequency_penalty,
        logit_bias = options.logit_bias,
        user = options.user,
        seed = options.seed
    }

    -- Add stop sequences if provided
    if options.stop_sequences then
        payload.stop = options.stop_sequences
    elseif options.stop then
        payload.stop = options.stop
    end

    -- Add thinking effort mapping - using the utility in openai client
    if args.thinking_effort and args.thinking_effort > 0 then
        payload.reasoning_effort = openai.map_thinking_effort(args.thinking_effort)
        payload.temperature = nil -- not working for reasoning models
    end

    -- Process tool schemas (either from tool_ids or direct tool_schemas)
    local request_tools = {}

    -- If tool IDs are provided, resolve them
    if args.tool_ids and #args.tool_ids > 0 then
        local tool_schemas, errors = tools.get_tool_schemas(args.tool_ids)
        if errors and next(errors) then
            local err_msg = "Failed to resolve tool schemas: "
            for id, err in pairs(errors) do
                err_msg = err_msg .. id .. " (" .. err .. "), "
            end
            return {
                error = output.ERROR_TYPE.INVALID_REQUEST,
                error_message = err_msg:sub(1, -3)  -- Remove trailing comma and space
            }
        end

        -- Convert tool schemas to OpenAI format
        for _, tool in pairs(tool_schemas) do
            table.insert(request_tools, {
                type = "function",
                function = {
                    name = tool.name,
                    description = tool.description,
                    parameters = tool.schema
                }
            })
        end
    end

    -- If tool schemas are provided directly, use them
    if args.tool_schemas and next(args.tool_schemas) then
        for _, tool in pairs(args.tool_schemas) do
            table.insert(request_tools, {
                type = "function",
                function = {
                    name = tool.name,
                    description = tool.description,
                    parameters = tool.schema
                }
            })
        end
    end

    -- Add tools to request if any are defined
    if #request_tools > 0 then
        payload.tools = request_tools

        -- Set tool_choice based on args.tool_call per spec
        if args.tool_call == "none" then
            payload.tool_choice = "none"
        elseif args.tool_call == "auto" or not args.tool_call then
            payload.tool_choice = "auto"
        elseif args.tool_call == "singular" then
            -- This is a specific requirement in the spec - forbids multiple tool calls
            -- For OpenAI, we'll set it to "required" which means at least one tool must be called
            payload.tool_choice = "required"
        elseif type(args.tool_call) == "string" and args.tool_call ~= "auto" and args.tool_call ~= "none" then
            -- A specific tool name was provided
            payload.tool_choice = {
                type = "function",
                function = {
                    name = args.tool_call
                }
            }
        end
    end

    -- Handle streaming configuration
    local stream_config = nil
    if args.stream then
        if type(args.stream) == "table" and args.stream.reply_to then
            stream_config = {
                enabled = true,
                reply_to = args.stream.reply_to,
                topic = args.stream.topic or "llm_response"
            }
            -- We'll handle streaming separately in a future implementation
        else
            return {
                error = output.ERROR_TYPE.INVALID_REQUEST,
                error_message = "Stream configuration requires reply_to process ID"
            }
        }
    end

    -- Make the request
    local request_options = {
        api_key = args.api_key,
        organization = args.organization,
        timeout = args.timeout or 120,
        base_url = args.endpoint
    }

    -- For now, we'll not implement streaming but validate the config
    if stream_config then
        return {
            error = output.ERROR_TYPE.INVALID_REQUEST,
            error_message = "Streaming not yet implemented for function calling"
        }
    }

    -- Perform the request to OpenAI
    local response, err = openai.request(
        openai.DEFAULT_CHAT_ENDPOINT,
        payload,
        request_options
    )

    -- Handle errors - use the map_error function to get standardized error format
    if err then
        return openai.map_error(err)
    }

    -- Check response validity
    if not response or not response.choices or #response.choices == 0 then
        return {
            error = output.ERROR_TYPE.SERVER_ERROR,
            error_message = "Invalid response structure from OpenAI"
        }
    }

    -- Extract the first choice
    local first_choice = response.choices[0] or response.choices[1]
    if not first_choice then
        return {
            error = output.ERROR_TYPE.SERVER_ERROR,
            error_message = "No choices in OpenAI response"
        }
    }

    -- Check if the response contains tool calls
    if first_choice.message and first_choice.message.tool_calls and #first_choice.message.tool_calls > 0 then
        local tool_calls = {}

        -- Process each tool call
        for _, tool_call in ipairs(first_choice.message.tool_calls) do
            if tool_call.function then
                table.insert(tool_calls, {
                    id = tool_call.id,
                    type = "function",
                    function = {
                        name = tool_call.function.name,
                        arguments = tool_call.function.arguments
                    }
                })
            end
        end

        -- Extract token usage information
        local tokens = nil
        if response.usage then
            tokens = output.usage(
                response.usage.prompt_tokens,
                response.usage.completion_tokens,
                (response.usage.completion_tokens_details and
                    response.usage.completion_tokens_details.reasoning_tokens) or 0
            )
        end

        -- Return tool call result
        return {
            type = output.TYPE.TOOL_CALL,
            provider = "openai",
            model = args.model,
            metadata = response.metadata,
            tool_calls = tool_calls,
            tokens = tokens,
            finish_reason = openai.FINISH_REASON_MAP[first_choice.finish_reason] or first_choice.finish_reason
        }
    elseif first_choice.message and first_choice.message.content then
        -- Extract content for normal text response
        local content = first_choice.message.content

        -- Extract token usage information
        local tokens = nil
        if response.usage then
            tokens = output.usage(
                response.usage.prompt_tokens,
                response.usage.completion_tokens,
                (response.usage.completion_tokens_details and
                    response.usage.completion_tokens_details.reasoning_tokens) or 0
            )
        end

        -- Use the finish reason map from the client
        local finish_reason = openai.FINISH_REASON_MAP[first_choice.finish_reason] or first_choice.finish_reason

        -- Return successful response with standardized finish reason
        return {
            result = content,
            tokens = tokens,
            metadata = response.metadata,
            finish_reason = finish_reason
        }
    else
        return {
            error = output.ERROR_TYPE.SERVER_ERROR,
            error_message = "No content or tool calls in OpenAI response"
        }
    }
end

return { handler = handler }