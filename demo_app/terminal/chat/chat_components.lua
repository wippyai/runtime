local openai_key = "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"

local M = {}

M.LLMClient = {
    new = function(model, endpoint)
        return {
            model = model or "o3-mini",
            endpoint = endpoint or "https://api.openai.com/v1/chat/completions",

            query = function(self, prompt, history, update_channel)
                coroutine.spawn(function()
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

                    local headers = { ["Content-Type"] = "application/json" }
                    if openai_key then
                        headers["Authorization"] = "Bearer " .. openai_key
                    end

                    local body = json.encode({
                        model = self.model,
                        messages = messages,
                        stream = true
                    })

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
                                if success and decoded and decoded.choices and decoded.choices[1] and decoded.choices[1].delta then
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

M.ChatSession = {
    new = function()
        return {
            messages = {},
            current_response = "",
            is_responding = false,
            current_message = nil,
            memory = "",

            add_message = function(self, role, content)
                local msg = { role = role, content = content }
                table.insert(self.messages, msg)
                return msg
            end,

            start_response = function(self)
                self.is_responding = true
                self.current_response = ""
                self.current_message = self:add_message("assistant", "")
            end,

            update_response = function(self, text)
                self.current_response = self.current_response .. text
                if self.current_message then
                    self.current_message.content = self.current_response
                end
            end,

            finish_response = function(self)
                if self.current_message then
                    self.current_message.content = self.current_response
                end
                self.current_response = ""
                self.is_responding = false
                self.current_message = nil
            end,

            clear = function(self)
                self.messages = {}
                self.current_response = ""
                self.is_responding = false
                self.current_message = nil
            end,

            get_history = function(self)
                return self.messages
            end,

            process_tool = function(self, prompt)
                local command, arg = prompt:match("^/(%w+)%s+(.*)")
                if command and command == "remember" then
                    self.memory = arg
                    self:add_message("assistant", "Memory updated!")
                    return true
                end
                return false
            end,

            get_memory = function(self)
                return self.memory ~= "" and self.memory or "No memory."
            end
        }
    end
}

return M
