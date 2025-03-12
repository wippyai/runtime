local openai = require("openai_client")
local output = require("output")
local json = require("json")
local env = require("env")

-- OpenAI Text Generation Handler
-- Basic completion without streaming support
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

    -- Perform the request to OpenAI
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

-- Return the handler function
return { handler = handler }
