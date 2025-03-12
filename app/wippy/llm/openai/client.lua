local json = require("json")
local http_client = require("http_client")
local env = require("env")
local output = require("wippy.llm:output")

-- OpenAI Client Library
local openai = {}

-- Constants
openai.DEFAULT_API_ENDPOINT = "https://api.openai.com/v1"
openai.DEFAULT_CHAT_ENDPOINT = "/chat/completions"
openai.DEFAULT_EMBEDDING_ENDPOINT = "/embeddings"

-- Error types mapping
openai.ERROR_TYPES = {
    invalid_request_error = output.ERROR_TYPE.INVALID_REQUEST,
    authentication_error = output.ERROR_TYPE.AUTHENTICATION,
    permission_error = output.ERROR_TYPE.AUTHENTICATION,
    rate_limit_error = output.ERROR_TYPE.RATE_LIMIT,
    server_error = output.ERROR_TYPE.SERVER_ERROR,
    context_length_exceeded = output.ERROR_TYPE.CONTEXT_LENGTH,
    content_filter = output.ERROR_TYPE.CONTENT_FILTER,
    timeout = output.ERROR_TYPE.TIMEOUT
}

-- Extract metadata from OpenAI HTTP response
local function extract_response_metadata(http_response)
    local metadata = {
        request_id = http_response.headers["x-request-id"],
        organization = http_response.headers["openai-organization"],
        processing_ms = tonumber(http_response.headers["openai-processing-ms"]),
        version = http_response.headers["openai-version"],
    }

    -- Add rate limit information if available
    local rate_limits = {}
    for header, value in pairs(http_response.headers) do
        if header:match("^x%-ratelimit") then
            local key = header:gsub("x%-ratelimit%-", ""):gsub("%-", "_")
            rate_limits[key] = tonumber(value) or value
        end
    end

    if next(rate_limits) then
        metadata.rate_limits = rate_limits
    end

    return metadata
end

-- Parse error from OpenAI response
local function parse_error(http_response)
    local error_info = {
        status_code = http_response.status_code,
        headers = {
            request_id = http_response.headers["x-request-id"]
        }
    }

    -- Try to parse error body as JSON
    local parsed, err = json.decode(http_response.body or "{}")
    if not err and parsed and parsed.error then
        error_info.type = openai.ERROR_TYPES[parsed.error.type] or output.ERROR_TYPE.SERVER_ERROR
        error_info.message = parsed.error.message or "Unknown OpenAI error"
        error_info.code = parsed.error.code
        error_info.param = parsed.error.param
        error_info.raw_type = parsed.error.type
    else
        error_info.type = output.ERROR_TYPE.SERVER_ERROR
        error_info.message = "OpenAI API error: " .. http_response.status_code
    end

    -- Add metadata from headers
    error_info.metadata = extract_response_metadata(http_response)

    return error_info
end

-- Make a request to the OpenAI API
function openai.request(endpoint_path, payload, options)
    options = options or {}

    -- Get API key
    local api_key = options.api_key or env.get("OPENAI_KEY")
    if not api_key then
        return nil, {
            type = output.ERROR_TYPE.AUTHENTICATION,
            message = "OpenAI API key is required",
            status_code = 401
        }
    end

    -- Prepare headers
    local headers = {
        ["Content-Type"] = "application/json",
        ["Authorization"] = "Bearer " .. api_key
    }

    -- Add organization header if specified
    local organization = options.organization or env.get("OPENAI_ORGANIZATION")
    if organization then
        headers["OpenAI-Organization"] = organization
    end

    -- Prepare endpoint URL
    local base_url = options.base_url or openai.DEFAULT_API_ENDPOINT
    local full_url = base_url .. endpoint_path

    -- Make the request
    local http_options = {
        headers = headers,
        body = json.encode(payload),
        timeout = options.timeout or 120
    }

    -- Handle streaming if requested
    if options.stream then
        http_options.stream = { buffer_size = options.buffer_size or 4096 }
    end

    -- Send the request
    local response = http_client.post(full_url, http_options)

    -- Handle streaming response
    if options.stream and response.stream then
        return {
            stream = response.stream,
            status_code = response.status_code,
            headers = response.headers,
            metadata = extract_response_metadata(response)
        }
    end

    -- Check for errors
    if response.status_code < 200 or response.status_code >= 300 then
        return nil, parse_error(response)
    end

    -- Parse successful response
    local parsed, err = json.decode(response.body)
    if err then
        return nil, {
            type = output.ERROR_TYPE.SERVER_ERROR,
            message = "Failed to parse OpenAI response: " .. err,
            status_code = response.status_code,
            metadata = extract_response_metadata(response)
        }
    end

    -- Add metadata to the response
    parsed.metadata = extract_response_metadata(response)

    return parsed
end

-- Chat completion function
function openai.chat_completion(messages, model, params)
    params = params or {}

    -- Prepare the request payload
    local payload = {
        model = model,
        messages = messages,
        temperature = params.temperature,
        top_p = params.top_p,
        n = params.n,
        stream = params.stream == true,
        max_tokens = params.max_tokens,
        presence_penalty = params.presence_penalty,
        frequency_penalty = params.frequency_penalty,
        logit_bias = params.logit_bias,
        user = params.user
    }

    -- Handle response_format if provided
    if params.response_format then
        payload.response_format = params.response_format
    end

    -- Add tool-related parameters if provided
    if params.tools and #params.tools > 0 then
        payload.tools = params.tools

        -- Set tool_choice if provided
        if params.tool_choice then
            payload.tool_choice = params.tool_choice
        end
    end

    -- Add reasoning_effort parameter for OpenAI reasoning models
    if params.reasoning_effort then
        payload.reasoning_effort = params.reasoning_effort
    end

    -- Add max_completion_tokens for OpenAI reasoning models
    if params.max_completion_tokens then
        payload.max_completion_tokens = params.max_completion_tokens
    end

    -- Make the request
    local response, err = openai.request(
        openai.DEFAULT_CHAT_ENDPOINT,
        payload,
        {
            api_key = params.api_key,
            organization = params.organization,
            timeout = params.timeout,
            stream = params.stream,
            buffer_size = params.buffer_size,
            base_url = params.base_url
        }
    )

    return response, err
end

-- Embeddings function
function openai.create_embeddings(input, model, params)
    params = params or {}

    -- Format input to ensure it's an array
    local input_text = input
    if type(input_text) ~= "table" then
        input_text = {input_text}
    end

    -- Prepare the payload
    local payload = {
        model = model,
        input = input_text,
        encoding_format = params.encoding_format or "float"
    }

    -- Add optional parameters
    if params.user then
        payload.user = params.user
    end

    if params.dimensions then
        payload.dimensions = params.dimensions
    end

    -- Make the request
    local response, err = openai.request(
        openai.DEFAULT_EMBEDDING_ENDPOINT,
        payload,
        {
            api_key = params.api_key,
            organization = params.organization,
            timeout = params.timeout,
            base_url = params.base_url
        }
    )

    return response, err
end

-- Process streaming response for chat completion
function openai.process_chat_stream(stream_response, stream_handler, options)
    options = options or {}
    local full_content = ""

    -- Handle streaming
    if stream_response and stream_response.stream then
        local has_error = false

        -- Process the stream
        while true do
            local chunk = stream_response.stream:read()
            if not chunk then break end

            for line in chunk:gmatch("[^\n]+") do
                if line:sub(1, 5) == "data:" then
                    local data = line:sub(6):match("^%s*(.-)%s*$")
                    if data == "[DONE]" then
                        break
                    end

                    local decoded, decode_err = json.decode(data)
                    if not decode_err and decoded then
                        -- Check for errors in the stream
                        if decoded.error then
                            if stream_handler and stream_handler.on_error then
                                stream_handler.on_error(decoded.error)
                            end
                            has_error = true
                            break
                        end

                        -- Process delta content
                        if decoded.choices and
                           decoded.choices[1] and
                           decoded.choices[1].delta then

                            local delta = decoded.choices[1].delta

                            -- Handle content
                            if delta.content then
                                if stream_handler and stream_handler.on_content then
                                    stream_handler.on_content(delta.content)
                                end
                                full_content = full_content .. delta.content
                            end

                            -- Handle tool calls
                            if delta.tool_calls then
                                if stream_handler and stream_handler.on_tool_call then
                                    stream_handler.on_tool_call(delta.tool_calls)
                                end
                            end
                        end
                    end
                end
            end

            if has_error then
                break
            end
        end

        -- Call done handler if available
        if stream_handler and stream_handler.on_done then
            stream_handler.on_done({
                content = full_content,
                metadata = stream_response.metadata,
                provider = "openai"
            })
        end

        -- Close the stream
        stream_response.stream:close()
    end

    return full_content
end

-- Function to format OpenAI response into standardized output format
function openai.format_completion_response(openai_response, params)
    if not openai_response or not openai_response.choices or #openai_response.choices == 0 then
        return nil, {
            type = output.ERROR_TYPE.SERVER_ERROR,
            message = "Invalid response structure from OpenAI"
        }
    end

    local result = {
        provider = "openai",
        model = params.model,
        metadata = openai_response.metadata,
        choices = {}
    }

    -- Add usage information if available
    if openai_response.usage then
        result.usage = {
            prompt_tokens = openai_response.usage.prompt_tokens,
            completion_tokens = openai_response.usage.completion_tokens,
            total_tokens = openai_response.usage.total_tokens
        }

        -- Add reasoning tokens if available
        if openai_response.usage.completion_tokens_details then
            result.usage.reasoning_tokens = openai_response.usage.completion_tokens_details.reasoning_tokens
        end
    end

    -- Process all choices
    for i, choice in ipairs(openai_response.choices) do
        local formatted_choice = {
            index = choice.index or (i - 1),
            message = choice.message,
            finish_reason = choice.finish_reason
        }

        -- Check for tool calls and reformat if needed
        if choice.message and choice.message.tool_calls and #choice.message.tool_calls > 0 then
            formatted_choice.tool_calls = {}

            for _, tool_call in ipairs(choice.message.tool_calls) do
                table.insert(formatted_choice.tool_calls, {
                    id = tool_call.id,
                    type = tool_call.type,
                    name = tool_call.["function"] and tool_call.["function"].name,
                    arguments = tool_call.["function"] and tool_call.["function"].arguments
                })
            end
        end

        table.insert(result.choices, formatted_choice)
    end

    return result
end

return openai