local time = require("time")
local json = require("json")
local http = require("http_client")
local env = require("env")

-- Claude API Client
local ClaudeClient = {}
ClaudeClient.__index = ClaudeClient

-- Constants
local API_BASE_URL = "https://api.anthropic.com/v1/messages"
local API_VERSION = "2023-06-01"
local DEFAULT_MODEL = "claude-3-opus-20240229"  -- Updated to use available model

function ClaudeClient.new(config)
    local self = setmetatable({}, ClaudeClient)

    -- Configuration
    self.api_key = config and config.api_key or env.get("ANTHROPIC_API_KEY")
    self.model = config and config.model or DEFAULT_MODEL
    self.max_tokens = config and config.max_tokens or 4096
    self.temperature = config and config.temperature or 0.7
    self.api_base_url = config and config.api_base_url or API_BASE_URL
    self.api_version = config and config.api_version or API_VERSION
    self.system_prompt = config and config.system_prompt or nil

    -- State
    self.messages = {}

    -- Validate API key
    if not self.api_key then
        error("ANTHROPIC_API_KEY environment variable or api_key config is required")
    end

    print("Claude client initialized with API key from " ..
        (config and config.api_key and "config" or "environment variable"))

    return self
end

-- Add a message to the conversation history
function ClaudeClient:add_message(role, content)
    if type(content) == "string" then
        content = { { type = "text", text = content } }
    end

    table.insert(self.messages, {
        role = role,
        content = content
    })

    return self
end

-- Clear conversation history
function ClaudeClient:clear_history()
    self.messages = {}
    return self
end

-- Prepare tools for API request
function ClaudeClient:prepare_tools(tools)
    local formatted_tools = {}

    for _, tool in ipairs(tools) do
        table.insert(formatted_tools, {
            name = tool.name,
            description = tool.description,
            input_schema = tool.input_schema
        })
    end

    return formatted_tools
end

-- Prepare request body
function ClaudeClient:prepare_request(options)
    local request = {
        model = options.model or self.model,
        max_tokens = options.max_tokens or self.max_tokens,
        temperature = options.temperature or self.temperature,
        messages = options.messages or self.messages
    }

    -- Add system prompt if provided
    if options.system or self.system_prompt then
        request.system = options.system or self.system_prompt
    end

    -- Add tools if provided
    if options.tools then
        request.tools = self:prepare_tools(options.tools)
    end

    -- Add tool_choice if provided
    if options.tool_choice then
        request.tool_choice = options.tool_choice
    end

    return request
end

-- Prepare headers for the API request
function ClaudeClient:prepare_headers()
    return {
        ["Content-Type"] = "application/json",
        ["x-api-key"] = self.api_key,
        ["anthropic-version"] = self.api_version
    }
end

-- Synchronous request to Claude API
function ClaudeClient:generate(options)
    local request_body = self:prepare_request(options or {})
    local headers = self:prepare_headers()

    -- Debug log
    print("Making API request to Claude with model: " .. (request_body.model or "default"))

    local response, err = http.post(self.api_base_url, {
        headers = headers,
        body = json.encode(request_body),
        timeout = 120
    })

    if err then
        print("API error: " .. err)
        return nil, "API request failed: " .. err
    end

    if response.status_code < 200 or response.status_code >= 300 then
        local body_text = response.body or ""
        print("API error status: " .. response.status_code .. " body: " .. body_text)
        return nil, "API error: " .. response.status_code .. " - " .. body_text
    end

    local result, parse_err = json.decode(response.body)
    if parse_err then
        return nil, "Failed to parse API response: " .. parse_err
    end

    -- Add the response to our conversation history
    if result.role and result.content then
        table.insert(self.messages, {
            role = result.role,
            content = result.content
        })
    end

    return result
end

-- Generate streaming response
function ClaudeClient:generate_stream(options, callback)
    local request_body = self:prepare_request(options or {})
    local headers = self:prepare_headers()

    -- Add streaming parameter
    request_body.stream = true

    -- Debug log
    print("Making streaming API request to Claude with model: " .. (request_body.model or "default"))

    local response, err = http.post(self.api_base_url, {
        headers = headers,
        body = json.encode(request_body),
        stream = { buffer_size = 4096 }
    })

    if err then
        print("API streaming error: " .. err)
        return nil, "API request failed: " .. err
    end

    if response.status_code < 200 or response.status_code >= 300 then
        local body_text = response.body or ""
        print("API error status: " .. response.status_code .. " body: " .. body_text)
        return nil, "API error: " .. response.status_code .. " - " .. body_text
    end

    -- Process the streaming response
    if not response.stream then
        return nil, "Streaming response expected but not available"
    end

    local stream = response.stream
    local full_response = { content = {} }

    -- Start reading from stream
    coroutine.spawn(function()
        while true do
            local chunk = stream:read()
            if err then
                callback({ type = "error", error = "Stream read error: " .. err })
                break
            end
            if not chunk then
                -- Stream ended
                callback({ type = "done", response = full_response })
                break
            end

            for line in chunk:gmatch("[^\n]+") do
                if line:sub(1, 6) == "data: " then
                    local data_line = line:sub(7):match("^%s*(.-)%s*$")
                    if data_line == "[DONE]" then
                        break
                    end
                    local success, event = pcall(json.decode, data_line)
                    if success and event then
                        -- Store the full response data
                        if event.type == "message_start" then
                            full_response.id = event.message.id
                            full_response.model = event.message.model
                            full_response.role = event.message.role
                            full_response.stop_reason = nil
                        elseif event.type == "content_block_start" then
                            table.insert(full_response.content, {
                                type = event.content_block.type,
                                text = ""
                            })
                        elseif event.type == "content_block_delta" then
                            local last_block = full_response.content[#full_response.content]
                            if last_block and last_block.type == "text" then
                                last_block.text = last_block.text .. (event.delta.text or "")
                                callback({
                                    type = "update",
                                    text = event.delta.text,
                                    index = #full_response.content,
                                    block_type = "text"
                                })
                            end
                        elseif event.type == "message_delta" and event.delta.stop_reason then
                            full_response.stop_reason = event.delta.stop_reason
                        elseif event.type == "tool_use" then
                            table.insert(full_response.content, {
                                type = "tool_use",
                                id = event.tool_use.id,
                                name = event.tool_use.name,
                                input = event.tool_use.input
                            })
                            callback({
                                type = "tool_use",
                                tool_use_id = event.tool_use.id,
                                name = event.tool_use.name,
                                input = event.tool_use.input
                            })
                        end
                    end
                end
            end
        end

        -- Clean up the stream
        stream:close()
    end)

    return { streaming = true }
end

-- Submit tool results to continue the conversation
function ClaudeClient:submit_tool_result(tool_use_id, result, error_message)
    -- Create the tool result content
    local content = {}
    if error_message then
        content = {
            {
                type = "tool_result",
                tool_use = {  -- Nested structure required by API
                    id = tool_use_id
                },
                content = error_message,
                status = "error"
            }
        }
    else
        content = {
            {
                type = "tool_result",
                tool_use = {  -- Nested structure required by API
                    id = tool_use_id
                },
                content = result
            }
        }
    end

    -- Add the user message with tool result
    table.insert(self.messages, {
        role = "user",
        content = content
    })

    return self
end

-- Extract text content from a Claude message
function ClaudeClient.extract_text(message)
    if not message or not message.content then
        return ""
    end

    local text = ""
    for _, block in ipairs(message.content) do
        if block.type == "text" then
            text = text .. block.text
        end
    end

    return text
end

-- Create a tool schema helper
function ClaudeClient.create_tool_schema(tool_name, description, properties, required_fields)
    local schema = {
        name = tool_name,
        description = description,
        input_schema = {
            type = "object",
            properties = properties,
            required = required_fields or {}
        }
    }

    return schema
end

return {
    Client = ClaudeClient
}