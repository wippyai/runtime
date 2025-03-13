local http_client = require("http_client")
local env = require("env")
local json = require("json")
local time = require("time")
local output = require("output")

-- Enhanced Claude API Client
local ClaudeClient = {}

-- Constants for API paths
ClaudeClient.API_ENDPOINTS = {
    MESSAGES = "/v1/messages"
}

-- Constants for model default parameters
ClaudeClient.MODEL_DEFAULTS = {
    -- Map models to their default max tokens
    ["claude-3-7-sonnet-20250219"] = 4096,
    ["claude-3-5-sonnet-20241022"] = 4096,
    ["claude-3-5-haiku-20241022"] = 4096,
    ["claude-3-opus-20240229"] = 4096,
    ["claude-3-sonnet-20240229"] = 4096,
    ["claude-3-haiku-20240307"] = 4096
}

-- Map Claude finish reasons to standardized finish reasons
ClaudeClient.FINISH_REASON_MAP = {
    ["end_turn"] = output.FINISH_REASON.STOP,
    ["max_tokens"] = output.FINISH_REASON.LENGTH,
    ["stop_sequence"] = output.FINISH_REASON.STOP,
    ["tool_use"] = output.FINISH_REASON.TOOL_CALL
}

-- Error type mapping function for Claude errors
function ClaudeClient.map_error(err)
    if not err then
        return {
            error = output.ERROR_TYPE.SERVER_ERROR,
            error_message = "Unknown error (nil error object)"
        }
    end

    -- Default to server error unless we determine otherwise
    local error_type = output.ERROR_TYPE.SERVER_ERROR

    -- Special cases for common error types based on status code
    if err.status_code == 401 then
        error_type = output.ERROR_TYPE.AUTHENTICATION
    elseif err.status_code == 404 then
        error_type = output.ERROR_TYPE.MODEL_ERROR
    elseif err.status_code == 429 then
        error_type = output.ERROR_TYPE.RATE_LIMIT
    elseif err.status_code >= 500 then
        error_type = output.ERROR_TYPE.SERVER_ERROR
    end

    -- Special cases based on error message content
    if err.message then
        -- Check for context length errors
        if err.message:match("context length") or
           err.message:match("maximum.+tokens") then
            error_type = output.ERROR_TYPE.CONTEXT_LENGTH
        end

        -- Check for content filter errors
        if err.message:match("content policy") or
           err.message:match("content filter") then
            error_type = output.ERROR_TYPE.CONTENT_FILTER
        end
    end

    -- Return in the format expected by the text generation handler
    return {
        error = error_type,
        error_message = err.message or "Unknown Claude API error"
    }
end

-- Extract metadata from Claude HTTP response
local function extract_response_metadata(http_response)
    if not http_response or not http_response.headers then
        return {}
    end

    local metadata = {
        -- Basic request information
        request_id = http_response.headers["x-request-id"],
        processing_ms = tonumber(http_response.headers["processing-ms"]),

        -- Rate limit information
        rate_limit = {
            requests_limit = tonumber(http_response.headers["anthropic-ratelimit-requests-limit"]),
            requests_remaining = tonumber(http_response.headers["anthropic-ratelimit-requests-remaining"]),
            requests_reset = http_response.headers["anthropic-ratelimit-requests-reset"],

            tokens_limit = tonumber(http_response.headers["anthropic-ratelimit-tokens-limit"]),
            tokens_remaining = tonumber(http_response.headers["anthropic-ratelimit-tokens-remaining"]),
            tokens_reset = http_response.headers["anthropic-ratelimit-tokens-reset"],

            input_tokens_limit = tonumber(http_response.headers["anthropic-ratelimit-input-tokens-limit"]),
            input_tokens_remaining = tonumber(http_response.headers["anthropic-ratelimit-input-tokens-remaining"]),
            input_tokens_reset = http_response.headers["anthropic-ratelimit-input-tokens-reset"],

            output_tokens_limit = tonumber(http_response.headers["anthropic-ratelimit-output-tokens-limit"]),
            output_tokens_remaining = tonumber(http_response.headers["anthropic-ratelimit-output-tokens-remaining"]),
            output_tokens_reset = http_response.headers["anthropic-ratelimit-output-tokens-reset"])
        },

        -- Additional headers that might be useful
        date = http_response.headers["Date"],
        content_type = http_response.headers["Content-Type"],
        retry_after = http_response.headers["retry-after"]
    }

    return metadata
end

-- Parse error from Claude response
local function parse_error(http_response)
    -- Always include status code to help with error type mapping
    local error_info = {
        status_code = http_response.status_code,
        message = "Claude API error: " .. (http_response.status_code or "unknown status")
    }

    -- Add request ID if available
    if http_response.headers and http_response.headers["x-request-id"] then
        error_info.request_id = http_response.headers["x-request-id"]
    end

    -- Try to parse error body as JSON
    if http_response.body then
        local parsed, decode_err = json.decode(http_response.body)
        if not decode_err and parsed and parsed.error then
            error_info.message = parsed.error.message or error_info.message
            error_info.type = parsed.error.type
        end
    end

    -- Add metadata from headers
    error_info.metadata = extract_response_metadata(http_response)

    return error_info
end

-- Process a streaming completion response
function ClaudeClient.process_stream(stream_response, callbacks)
    if not stream_response or not stream_response.stream then
        return nil, "Invalid stream response"
    end

    local full_content = ""
    local finish_reason = nil
    local stop_sequence = nil
    local usage = {}
    local metadata = stream_response.metadata or {}
    local content_blocks = {}
    local tool_calls = {}

    -- Default callbacks
    callbacks = callbacks or {}
    local on_content = callbacks.on_content or function() end
    local on_tool_call = callbacks.on_tool_call or function() end
    local on_thinking = callbacks.on_thinking or function() end
    local on_error = callbacks.on_error or function() end
    local on_done = callbacks.on_done or function() end

    -- Process each streamed chunk
    while true do
        local chunk, err = stream_response.stream:read()

        -- Handle read errors
        if err then
            on_error(err)
            return nil, err
        end

        -- End of stream
        if not chunk then
            break
        end

        -- Skip empty chunks
        if chunk == "" then
            goto continue
        end

        -- Extract and process each event line
        for event_line in chunk:gmatch("event: (.-)\n") do
            local event_type = event_line:match("^(%S+)")
            local data_line = chunk:match("data: (.-)\n")

            if not data_line then
                goto continue_event
            end

            -- Parse the data as JSON
            local data, parse_err = json.decode(data_line)
            if parse_err then
                goto continue_event
            end

            -- Handle different event types
            if event_type == "message_start" then
                -- Store initial usage information
                if data.message and data.message.usage then
                    usage = data.message.usage
                end
            elseif event_type == "content_block_delta" then
                -- Handle content block delta
                local block_index = data.index
                local delta = data.delta

                if delta.type == "text_delta" then
                    -- Text content
                    local content_chunk = delta.text
                    full_content = full_content .. content_chunk
                    on_content(content_chunk)
                elseif delta.type == "thinking_delta" then
                    -- Thinking content
                    on_thinking(delta.thinking)
                elseif delta.type == "input_json_delta" then
                    -- Tool use content
                    local tool_call_index = data.index

                    -- Initialize tool call if needed
                    if not tool_calls[tool_call_index] then
                        tool_calls[tool_call_index] = {
                            partial_json = ""
                        }
                    end

                    -- Accumulate JSON
                    tool_calls[tool_call_index].partial_json =
                        tool_calls[tool_call_index].partial_json ..
                        (delta.partial_json or "")
                end
            elseif event_type == "content_block_stop" then
                -- A content block has been completed
                local block_index = data.index

                -- If this is a completed tool call, try to parse it
                if tool_calls[block_index] and tool_calls[block_index].partial_json then
                    local json_str = tool_calls[block_index].partial_json
                    local success, parsed_input = pcall(json.decode, json_str)

                    if success and parsed_input then
                        -- Get the tool call details from content_blocks data
                        if content_blocks[block_index] and
                           content_blocks[block_index].type == "tool_use" then
                            local tool_call = content_blocks[block_index]
                            on_tool_call({
                                id = tool_call.id,
                                name = tool_call.name,
                                arguments = parsed_input
                            })
                        end
                    end
                end
            elseif event_type == "content_block_start" then
                -- Store content block information
                content_blocks[data.index] = data.content_block
            elseif event_type == "message_delta" then
                -- Update finish reason and usage
                if data.delta then
                    finish_reason = data.delta.stop_reason
                    stop_sequence = data.delta.stop_sequence
                end

                if data.usage then
                    -- Update usage information
                    for k, v in pairs(data.usage) do
                        usage[k] = v
                    end
                end
            elseif event_type == "message_stop" then
                -- End of message, create final result
                local result = {
                    content = full_content,
                    finish_reason = finish_reason,
                    stop_sequence = stop_sequence,
                    tool_calls = next(tool_calls) and tool_calls or nil,
                    usage = usage,
                    metadata = metadata
                }

                -- Call the done callback
                on_done(result)
                return full_content, nil, result
            elseif event_type == "error" then
                -- Handle error events
                if data and data.error then
                    local error_info = {
                        message = data.error.message,
                        type = data.error.type
                    }
                    on_error(error_info)
                    return nil, error_info.message, { error = error_info }
                end
            end

            ::continue_event::
        end

        ::continue::
    end

    -- Create the final result if we didn't get a message_stop event
    local result = {
        content = full_content,
        finish_reason = finish_reason,
        stop_sequence = stop_sequence,
        tool_calls = next(tool_calls) and tool_calls or nil,
        usage = usage,
        metadata = metadata
    }

    -- Call the done callback
    on_done(result)

    return full_content, nil, result
end

-- Extract usage information from response
function ClaudeClient.extract_usage(claude_response)
    if not claude_response or not claude_response.usage then
        return nil
    end

    local usage = {
        prompt_tokens = claude_response.usage.input_tokens or 0,
        completion_tokens = claude_response.usage.output_tokens or 0,
        total_tokens = (claude_response.usage.input_tokens or 0) +
                       (claude_response.usage.output_tokens or 0)
    }

    -- Add cache tokens if available
    if claude_response.usage.cache_creation_input_tokens then
        usage.cache_creation_input_tokens = claude_response.usage.cache_creation_input_tokens
    end

    if claude_response.usage.cache_read_input_tokens then
        usage.cache_read_input_tokens = claude_response.usage.cache_read_input_tokens
    end

    return usage
end

function ClaudeClient.new(api_key)
    local client = {}

    -- Constants
    client.API_URL = "https://api.anthropic.com"
    client.API_VERSION = "2023-06-01"
    client.MODEL = "claude-3-7-sonnet-20250219"
    client.MAX_TOKENS = ClaudeClient.MODEL_DEFAULTS[client.MODEL] or 4096

    -- Thinking mode configuration
    client.thinking_enabled = false
    client.thinking_budget = 1024  -- Default minimum budget

    -- Configuration
    client.api_key = api_key or env.get("ANTHROPIC_API_KEY")
    client.system_prompt = nil

    -- Beta features
    client.beta_features = {}

    -- Send a request to Claude API
    client.send_request = function(self, endpoint, payload, options)
        options = options or {}

        -- Prepare headers
        local headers = {
            ["Content-Type"] = "application/json",
            ["x-api-key"] = self.api_key,
            ["anthropic-version"] = self.API_VERSION
        }

        -- Add beta features if enabled
        if next(self.beta_features) then
            headers["anthropic-beta"] = table.concat(self.beta_features, ",")
        end

        -- Full URL
        local url = self.API_URL .. endpoint

        -- HTTP options
        local http_options = {
            headers = headers,
            timeout = options.timeout or 120
        }

        -- Enable streaming if requested
        if options.stream then
            http_options.stream = { buffer_size = 4096 }
            payload.stream = true
        end

        -- Encode payload
        local payload_json, err = json.encode(payload)
        if err then
            return nil, {
                status_code = 400,
                message = "Failed to encode request: " .. err
            }
        end

        http_options.body = payload_json

        -- Log request for debugging (only when needed)
        if options.debug then
            local timestamp = time.now():format("20060102_150405")
            local debug_file = "claude_request_" .. timestamp .. ".json"
            require("fs").get("system:core"):writefile(debug_file, payload_json)
        end

        -- Make the request
        local response, err = http_client.post(url, http_options)

        -- Handle request errors
        if err then
            return nil, {
                status_code = 0,
                message = "HTTP request failed: " .. err
            }
        end

        -- Handle HTTP error status codes
        if response.status_code < 200 or response.status_code >= 300 then
            local error_info = parse_error(response)

            -- Log error payload for debugging if enabled
            if options.debug then
                local timestamp = time.now():format("20060102_150405")
                local debug_file = "claude_error_" .. timestamp .. ".json"
                require("fs").get("system:core"):writefile(debug_file, payload_json)
            end

            return nil, error_info
        end

        -- Handle streaming response
        if options.stream and response.stream then
            return {
                stream = response.stream,
                status_code = response.status_code,
                headers = response.headers,
                metadata = extract_response_metadata(response)
            }
        end

        -- Parse successful response
        local success, parsed = pcall(json.decode, response.body)
        if not success then
            return nil, {
                status_code = response.status_code,
                message = "Failed to parse Claude response: " .. parsed,
                metadata = extract_response_metadata(response)
            }
        end

        -- Add metadata to the response
        parsed.metadata = extract_response_metadata(response)

        return parsed
    end

    -- Set beta features
    client.enable_beta = function(self, feature_name)
        table.insert(self.beta_features, feature_name)
        return self
    end

    -- Send a message using the Messages API
    client.create_message = function(self, options)
        options = options or {}

        -- Build request payload
        local payload = {
            model = options.model or self.MODEL,
            max_tokens = options.max_tokens or self.MAX_TOKENS,
            messages = options.messages or {},
            temperature = options.temperature,
            system = options.system or self.system_prompt,
            stop_sequences = options.stop_sequences,
            stream = options.stream and true or nil
        }

        -- Add thinking configuration if enabled
        if self.thinking_enabled or options.thinking_enabled then
            payload.thinking = {
                type = "enabled",
                budget_tokens = options.thinking_budget or self.thinking_budget
            }
        end

        -- Add tools if provided
        if options.tools and #options.tools > 0 then
            payload.tools = options.tools

            -- Set tool_choice based on options
            if options.tool_choice then
                payload.tool_choice = options.tool_choice
            end
        end

        -- Handle streaming
        local stream_options = nil
        if options.stream_handler then
            stream_options = {
                stream = true
            }
        end

        -- Send request
        local response, err = self:send_request(
            ClaudeClient.API_ENDPOINTS.MESSAGES,
            payload,
            {
                stream = options.stream,
                debug = options.debug,
                timeout = options.timeout
            }
        )

        -- Handle errors
        if err then
            return nil, err
        end

        -- Handle streaming if a handler is provided
        if options.stream and options.stream_handler and response.stream then
            return ClaudeClient.process_stream(response, options.stream_handler)
        end

        return response
    end

    -- Enable/disable thinking mode
    client.set_thinking_enabled = function(self, enabled)
        self.thinking_enabled = enabled and true or false
        return self
    end

    -- Set thinking budget
    client.set_thinking_budget = function(self, budget)
        -- Ensure minimum budget is 1024 tokens as per documentation
        self.thinking_budget = math.max(1024, tonumber(budget) or 1024)
        return self
    end

    -- Configure client
    client.configure = function(self, options)
        if options.api_key then
            self.api_key = options.api_key
        end

        if options.model then
            self.MODEL = options.model
            -- Update max tokens based on model if not explicitly set
            if not options.max_tokens then
                self.MAX_TOKENS = ClaudeClient.MODEL_DEFAULTS[self.MODEL] or 4096
            end
        end

        if options.system_prompt then
            self.system_prompt = options.system_prompt
        end

        if options.max_tokens then
            self.MAX_TOKENS = options.max_tokens
        end

        if options.thinking_enabled ~= nil then
            self.thinking_enabled = options.thinking_enabled
        end

        if options.thinking_budget then
            self:set_thinking_budget(options.thinking_budget)
        end

        if options.api_version then
            self.API_VERSION = options.api_version
        end

        return self
    end

    return client
end

return ClaudeClient