local openai = require("openai_client")
local output = require("output")
local json = require("json")
local env = require("env")

-- OpenAI Text Generation Handler
-- Supports both streaming and non-streaming completion
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
    end

    -- Configure options objects for easier management
    local options = args.options or {}

    -- Configure request payload
    local payload = {
        model = args.model,
        messages = messages,
        temperature = options.temperature,
        top_p = options.top_p,
        n = options.n,
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
    if options.thinking_effort then
        payload.reasoning_effort = openai.map_thinking_effort(options.thinking_effort)
        payload.temperature = nil -- not working for reasoning models
    end

    -- Add max_completion_tokens
    if options.max_completion_tokens then
        payload.max_completion_tokens = options.max_completion_tokens
    end

    -- Make the request
    local request_options = {
        api_key = args.api_key,
        organization = args.organization,
        timeout = args.timeout or 120,
        base_url = args.endpoint
    }

    -- Handle streaming if requested
    if args.stream and args.stream.reply_to then
        -- Enable streaming in the request
        request_options.stream = true

        -- Create a streamer with the provided reply_to process ID
        local streamer = output.streamer(
            args.stream.reply_to,
            args.stream.topic or "llm_response",
            args.stream.buffer_size or 10
        )

        -- Perform the streaming request
        local response, err = openai.request(
            openai.DEFAULT_CHAT_ENDPOINT,
            payload,
            request_options
        )

        -- Handle request errors
        if err then
            local mapped_error = openai.map_error(err)
            streamer:send_error(
                mapped_error.error,
                mapped_error.error_message,
                mapped_error.code
            )
            return mapped_error
        end

        -- Variables to store the full result
        local full_content = ""
        local finish_reason = nil

        -- Process the streaming response
        full_content = openai.process_stream(response, {
            on_content = function(content_chunk)
                streamer:buffer_content(content_chunk)
            end,
            on_error = function(error_info)
                streamer:send_error(
                    output.ERROR_TYPE.SERVER_ERROR,
                    error_info.message or "Error processing stream",
                    error_info.code
                )
            end,
            on_done = function(result)
                -- Flush any remaining content
                streamer:flush()

                -- Save finish reason
                if result.finish_reason then
                    finish_reason = result.finish_reason
                end

                -- Send a simplified done message with minimal info
                streamer:send_done({
                    model = args.model,
                    provider = "openai"
                })
            end
        })

        -- Return the final content and metadata for the caller
        return {
            result = full_content,
            tokens = nil, -- Tokens are sent in the done event
            metadata = response.metadata,
            finish_reason = finish_reason and openai.FINISH_REASON_MAP[finish_reason] or finish_reason,
            streaming = true
        }
    else
        -- Non-streaming request
        local response, err = openai.request(
            openai.DEFAULT_CHAT_ENDPOINT,
            payload,
            request_options
        )

        -- Handle errors - use the map_error function to get standardized error format
        if err then
            return openai.map_error(err)
        end

        -- Check response validity
        if not response or not response.choices or #response.choices == 0 then
            return {
                error = output.ERROR_TYPE.SERVER_ERROR,
                error_message = "Invalid response structure from OpenAI"
            }
        end

        -- Extract content from first choice
        local first_choice = response.choices[1]
        local content = nil
        if first_choice and first_choice.message and first_choice.message.content then
            content = first_choice.message.content
        else
            return {
                error = output.ERROR_TYPE.SERVER_ERROR,
                error_message = "No content in OpenAI response"
            }
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

        -- Use the finish reason map from the client
        local finish_reason = openai.FINISH_REASON_MAP[first_choice.finish_reason] or first_choice.finish_reason

        -- Return successful response with standardized finish reason
        return {
            result = content,
            tokens = tokens,
            metadata = response.metadata,
            finish_reason = finish_reason
        }
    end
end

-- Return the handler function
return { handler = handler }
