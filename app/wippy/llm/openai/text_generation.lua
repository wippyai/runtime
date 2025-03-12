local openai = require("openai_client")
local output = require("output")
local json = require("json")

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
        return {
            error = output.ERROR_TYPE.INVALID_REQUEST,
            error_message = "No messages provided"
        }
    end

    -- Configure request payload
    local payload = {
        model = args.model,
        messages = messages,
        temperature = args.temperature,
        top_p = args.top_p,
        n = args.n,
        max_tokens = args.max_tokens,
        presence_penalty = args.presence_penalty,
        frequency_penalty = args.frequency_penalty,
        logit_bias = args.logit_bias,
        user = args.user
    }

    -- Handle response_format if provided
    if args.response_format == "json" then
        payload.response_format = { type = "json_object" }
    end

    -- Add reasoning_effort parameter for OpenAI reasoning models
    if args.thinking_effort then
        payload.reasoning_effort = args.thinking_effort
    end

    -- Add max_completion_tokens for OpenAI reasoning models
    if args.max_completion_tokens then
        payload.max_completion_tokens = args.max_completion_tokens
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

    -- Handle errors
    if err then
        return {
            error = err.type,
            error_message = err.message,
            status_code = err.status_code
        }
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

    -- Return successful response
    return {
        result = content,
        tokens = tokens,
        metadata = response.metadata,
        finish_reason = first_choice.finish_reason
    }
end

-- Return the handler function
return { handler = handler }
