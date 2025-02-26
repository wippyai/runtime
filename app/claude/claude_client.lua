local http_client = require("http_client")
local env = require("env")
local json = require("json")
local time = require("time")

-- Claude API Client
local ClaudeClient = {}

function ClaudeClient.new(api_key)
    local client = {}

    -- Constants
    client.API_URL = "https://api.anthropic.com/v1/messages"
    client.API_VERSION = "2023-06-01"
    client.MODEL = "claude-3-5-haiku-20241022" --"claude-3-7-sonnet-20250219" todo: make it properly selectable
    client.MAX_TOKENS = 4096

    -- Configuration
    client.api_key = api_key or env.get("ANTHROPIC_API_KEY")
    client.system_prompt = [[
You are Wippy, an AI assistant with access to file system tools.
You can help users interact with their files and directories.
When a user asks about files or directories, use the appropriate tool to help them.
]]

    -- Send a request to Claude API
    client.send_request = function(self, tools, messages, on_response)
        -- Prepare request
        local request = {
            model = self.MODEL,
            max_tokens = self.MAX_TOKENS,
            messages = messages,
            system = self.system_prompt,
            tools = tools,
            stream = true
        }

        -- Headers
        local headers = {
            ["Content-Type"] = "application/json",
            ["x-api-key"] = self.api_key,
            ["anthropic-version"] = self.API_VERSION
        }

        -- Make API request
        coroutine.spawn(function()
            local payload, err = json.encode(request)
            if err then
                on_response("Failed to encode request: " .. err)
                return nil, "Failed to encode request: " .. err
            end

            local response, err = http_client.post(self.API_URL, {
                headers = headers,
                body = payload,
                stream = { buffer_size = 4096 }
            })

            if err then
                return nil, "API request failed: " .. err
            end

            if response.status_code < 200 or response.status_code >= 300 then
                -- Get full error response
                local error_body = ""
                if response.body then
                    error_body = response.body
                elseif response.stream then
                    local chunk = response.stream:read()
                    if chunk then
                        error_body = chunk
                    end
                    response.stream:close()
                end

                local timestamp = time.now():format("20060102_150405")
                local debug_file = "payload_error_" .. timestamp .. ".json"

                -- Dump to file for full inspection
                require("fs").get("system:core"):writefile(debug_file, payload)

                on_response(error_body)

                return nil, "API error: " .. response.status_code .. " - " .. error_body
            end

            -- Process stream with callback
            if on_response then
                on_response(response.stream)
            else
                response.stream:close()
            end
        end)
    end

    -- Configure client
    client.configure = function(self, options)
        if options.api_key then
            self.api_key = options.api_key
        end

        if options.model then
            self.MODEL = options.model
        end

        if options.system_prompt then
            self.system_prompt = options.system_prompt
        end

        if options.max_tokens then
            self.MAX_TOKENS = options.max_tokens
        end

        return self
    end

    return client
end

return ClaudeClient
