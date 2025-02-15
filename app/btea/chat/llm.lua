local time = require("time")
local json = require("json")
local http = require("http_client")
local env = require("env")

local M = {}

M.LLMClient = {
    new = function(model, endpoint)
        return {
            model = model or "o3-mini",
            endpoint = endpoint or "https://api.openai.com/v1/chat/completions",

            query = function(self, prompt, history, update_channel)
                coroutine.spawn(function()
                    -- Format messages
                    local messages = {}
                    for _, msg in ipairs(history) do
                        table.insert(messages, {
                            role = msg.role,
                            content = msg.content
                        })
                    end
                    table.insert(messages, {
                        role = "user",
                        content = prompt
                    })
                    --

                    ---- Prepare headers
                    local headers = {
                        ["Content-Type"] = "application/json",
                        ["Authorization"] = "Bearer " .. (env.get("OPENAI_KEY") or "")
                    }
                    --

                    ------ Prepare request body
                    local body = json.encode({
                        model = self.model,
                        messages = messages,
                        stream = true
                    })
                    --
                    ---- Make API request
                    local response, err = http.post(self.endpoint, {
                        headers = headers,
                        body = body,
                        stream = { buffer_size = 4096 }
                    })

                    if err then
                        update_channel:send({ type = "error", error = err })
                        return
                    end

                    if not response or not response.stream then
                        update_channel:send({ type = "error", error = "No stream available" })
                        return
                    end

                    local stream = response.stream
                    local buffer = ""

                    while true do
                        local chunk, err = stream:read()
                        if err then break end
                        if not chunk then break end

                        for line in chunk:gmatch("[^\n]+") do
                            if line:sub(1, 5) == "data:" then
                                local data_line = line:sub(6):match("^%s*(.-)%s*$")
                                if data_line == "[DONE]" then
                                    break
                                end
                                local success, decoded = pcall(json.decode, data_line)
                                if success and decoded and
                                    decoded.choices and
                                    decoded.choices[1] and
                                    decoded.choices[1].delta then
                                    local content = decoded.choices[1].delta.content
                                    if content then
                                        buffer = buffer .. content
                                        if buffer:match("[%.%!%?]%s*$") or #buffer > 10 then
                                            update_channel:send({ type = "update", text = buffer })
                                            buffer = ""
                                            time.sleep("30ms")
                                        end
                                    end
                                end
                            end
                        end
                    end

                    if #buffer > 0 then
                        update_channel:send({ type = "update", text = buffer })
                    end
                    stream:close()
                    update_channel:send({ type = "done" })
                end)
            end
        }
    end
}

return M
