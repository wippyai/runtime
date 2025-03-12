local openai = require("openai_client")
local output = require("output")
local tools = require("tools")

-- OpenAI Tool Calling Handler
-- Supports both streaming and non-streaming tool calling
local function handler(args)
    -- Validate required arguments
    if not args.model then
        return nil, "Model is required"
    end

    if not (args.tool_ids or args.tool_schemas or args.schema) then
        return nil, "Either tool_ids, tool_schemas, or schema is required for tool/function calling"
    end

    -- Format messages from various input formats
    local messages = {}

    -- If messages array provided directly, use it
    if args.messages and #args.messages > 0 then
        messages = args.messages
    else
        -- Otherwise build from separate fields
        -- Add system prompt if provided
        if args.system_prompt then
            table.insert(messages, {
                role = "system",
                content = args.system_prompt
            })
        end

        -- Add user message
        if args.message then
            table.insert(messages, {
                role = "user",
                content = args.message
            })
        end
    end

    if #messages == 0 then
        return nil, "No messages provided"
    end

    -- Configure parameters for OpenAI request
    local params = {
        model = args.model,
        temperature = args.temperature,
        top_p = args.top_p,
        frequency_penalty = args.frequency_penalty,
        presence_penalty = args.presence_penalty,
        max_tokens = args.max_tokens,
        timeout = args.timeout,
        api_key = args.api_key,
        organization = args.organization,
        base_url = args.endpoint and args.endpoint:match("(.-)/*$"),
        reasoning_effort = args.reasoning_effort, -- For o1/o3 models
        max_completion_tokens = args.max_completion_tokens -- For o1/o3 models
    }

    -- Handle schema-based approach if specified
    if args.schema then
        -- For schema-based responses, we're forcing a JSON structure
        params.response_format = { type = "json_object" }

        -- Add schema enforcement through a system message
        local schema_message = {
            role = "system",
            content = "You MUST respond with a JSON object that conforms to the following schema: " ..
                      (type(args.schema) == "string" and args.schema or require("json").encode(args.schema))
        }

        -- Insert at the beginning of messages, preserving existing system messages
        table.insert(messages, 1, schema_message)
    else
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
                return nil, err_msg:sub(1, -3)  -- Remove trailing comma and space
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

        -- Add tools to request
        if #request_tools > 0 then
            params.tools = request_tools

            -- Set tool_choice based on args
            if args.tool_call_behavior == "none" then
                params.tool_choice = "none"
            elseif args.tool_call_behavior == "auto" or not args.tool_call_behavior then
                params.tool_choice = "auto"
            else
                -- If a specific tool is requested
                params.tool_choice = {
                    type = "function",
                    function = {
                        name = args.tool_call_behavior
                    }
                }
            end
        end
    end

    -- Handle streaming
    if args.stream and args.stream_to then
        params.stream = true
        params.buffer_size = args.buffer_size or 4096

        -- Create a streamer
        local streamer = output.streamer(args.stream_to, args.topic or "llm_response", args.buffer_size or 10)

        -- Make streaming request
        local stream_response, err = openai.chat_completion(messages, args.model, params)

        if err then
            streamer:send_error(
                err.type or output.ERROR_TYPE.SERVER_ERROR,
                err.message or "OpenAI API error",
                err.status_code
            )
            return nil, err.message, { error = err }
        end

        -- Keep track of tool calls
        local tool_calls = {}
        local tool_call_index = 0
        local current_tool_call = nil

        -- Process the streaming response
        local full_content = openai.process_chat_stream(stream_response, {
            on_content = function(content)
                streamer:buffer_content(content)
            end,
            on_tool_call = function(delta_tool_calls)
                for _, delta in ipairs(delta_tool_calls) do
                    -- If this is a new tool call, initialize it
                    if delta.index and (not current_tool_call or delta.index ~= tool_call_index) then
                        tool_call_index = delta.index
                        current_tool_call = {
                            id = delta.id,
                            type = delta.type,
                            function = {
                                name = delta.function and delta.function.name or "",
                                arguments = delta.function and delta.function.arguments or ""
                            }
                        }
                        tool_calls[tool_call_index + 1] = current_tool_call
                    elseif current_tool_call then
                        -- Update existing tool call with delta data
                        if delta.function then
                            if delta.function.name then
                                current_tool_call.function.name =
                                    current_tool_call.function.name .. delta.function.name
                            end
                            if delta.function.arguments then
                                current_tool_call.function.arguments =
                                    current_tool_call.function.arguments .. delta.function.arguments
                            end
                        end
                    end

                    -- If we have a complete tool call, send it
                    if current_tool_call and current_tool_call.function.name and
                       current_tool_call.function.arguments then
                        streamer:send_tool_call(
                            current_tool_call.function.name,
                            current_tool_call.function.arguments,
                            current_tool_call.id
                        )
                    end
                end
            end,
            on_error = function(error_info)
                streamer:send_error(
                    output.ERROR_TYPE.SERVER_ERROR,
                    error_info.message or "OpenAI streaming error"
                )
            end,
            on_done = function(result)
                -- Flush any remaining content
                streamer:flush()

                -- Send done event with metadata
                streamer:send_done({
                    model = args.model,
                    provider = "openai",
                    request_id = result.metadata and result.metadata.request_id,
                    tool_calls = #tool_calls > 0 and tool_calls or nil
                })
            end
        })

        -- Return appropriate response based on what happened
        if #tool_calls > 0 then
            -- Return tool call result
            return {
                type = output.TYPE.TOOL_CALL,
                provider = "openai",
                model = args.model,
                metadata = stream_response.metadata,
                tool_calls = tool_calls
            }
        else
            -- Return the full content that was streamed
            return full_content, nil, {
                provider = "openai",
                model = args.model,
                metadata = stream_response.metadata
            }
        end
    else
        -- Non-streaming request
        local response, err = openai.chat_completion(messages, args.model, params)

        if err then
            return nil, err.message, { error = err }
        end

        -- Format the response in our standardized format
        local formatted_response = openai.format_completion_response(response, {
            model = args.model
        })

        if not formatted_response then
            return nil, "Failed to format OpenAI response"
        end

        -- Check if the response contains tool calls
        local first_choice = formatted_response.choices[1]
        if first_choice and first_choice.tool_calls and #first_choice.tool_calls > 0 then
            -- Return tool call result
            return {
                type = output.TYPE.TOOL_CALL,
                provider = "openai",
                model = args.model,
                metadata = formatted_response.metadata,
                tool_calls = first_choice.tool_calls
            }
        elseif first_choice and first_choice.message and first_choice.message.content then
            -- Regular text response
            return first_choice.message.content, nil, formatted_response
        else
            return nil, "No content or tool calls in OpenAI response"
        end
    end
end

-- Return the handler function
return { handler = handler }