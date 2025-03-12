local openai = require("openai_client")
local output = require("wippy.llm:output")

-- OpenAI Text Generation Handler
-- Supports both streaming and non-streaming modes
local function handler(args)
    -- Validate required arguments
    if not args.model then
        return nil, "Model is required"
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

    -- Handle JSON mode if requested
    if args.response_format == "json" then
        params.response_format = { type = "json_object" }
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

        -- Process the streaming response
        local full_content = openai.process_chat_stream(stream_response, {
            on_content = function(content)
                streamer:buffer_content(content)
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
                    request_id = result.metadata and result.metadata.request_id
                })
            end
        })

        -- Return the full content that was streamed
        return full_content, nil, {
            provider = "openai",
            model = args.model,
            metadata = stream_response.metadata
        }
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

        -- Extract the text content from the first choice
        local first_choice = formatted_response.choices[1]
        if first_choice and first_choice.message and first_choice.message.content then
            return first_choice.message.content, nil, formatted_response
        else
            return nil, "No content in OpenAI response"
        end
    end
end

-- Return the handler function
return { handler = handler }