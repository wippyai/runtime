local openai_key = "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"

local M = {}

M.LLMClient = {
    new = function(model, endpoint)
        return {
            model = model or "o3-mini",
            endpoint = endpoint or "https://api.openai.com/v1/chat/completions",

            query = function(self, prompt, history, update_channel, log_fn)
                if log_fn then log_fn("LLMClient: starting query") end
                coroutine.spawn(function()
                    -- Convert history into messages array
                    local messages = {}
                    for _, msg in ipairs(history) do
                        table.insert(messages, {
                            role = msg.role,
                            content = msg.content
                        })
                    end
                    -- Add current prompt
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
                        if log_fn then log_fn("LLMClient error (http.post): " .. tostring(err)) end
                        update_channel:send({ type = "error", error = err })
                        return
                    end

                    if not response or not response.stream then
                        if log_fn then log_fn("LLMClient: no streaming response received") end
                        update_channel:send({ type = "error", error = "No stream available" })
                        return
                    end

                    local stream = response.stream
                    local buffer = ""
                    if log_fn then log_fn("LLMClient: stream received") end

                    while true do
                        local chunk, err = stream:read()
                        if err then
                            if log_fn then log_fn("LLMClient: error reading stream: " .. tostring(err)) end
                            break
                        end
                        if not chunk then
                            if log_fn then log_fn("LLMClient: stream ended") end
                            break
                        end

                        -- Minimal logging per chunk
                        if log_fn then log_fn("LLMClient: received chunk") end

                        for line in chunk:gmatch("[^\n]+") do
                            if line:sub(1, 5) == "data:" then
                                local data_line = line:sub(6):match("^%s*(.-)%s*$")  -- trim whitespace
                                if data_line == "[DONE]" then
                                    break
                                end
                                local success, decoded = pcall(json.decode, data_line)
                                if success and decoded and decoded.choices and decoded.choices[1] and decoded.choices[1].delta then
                                    local content = decoded.choices[1].delta.content
                                    if content then
                                        buffer = buffer .. content
                                        if buffer:match("[%.%!%?]%s*$") or #buffer > 50 then
                                            update_channel:send({ type = "update", text = buffer })
                                            if log_fn then log_fn("LLMClient: sending update") end
                                            buffer = ""
                                            time.sleep("30ms")
                                        end
                                    end
                                else
                                    if log_fn then log_fn("LLMClient: failed to decode line") end
                                end
                            end
                        end
                    end

                    if #buffer > 0 then
                        update_channel:send({ type = "update", text = buffer })
                        if log_fn then log_fn("LLMClient: sending final update") end
                    end
                    stream:close()
                    update_channel:send({ type = "done" })
                    if log_fn then log_fn("LLMClient: query finished") end
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
            memory = "",  -- New memory field

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

            -- Tool: remember something. This command is triggered when user input starts with "/remember ".
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

M.ChatUI = {
    new = function(width, height)
        width = width or 104
        height = height or 48

        local COLORS = {
            THINKING = "\27[33m", -- Yellow for thinking
            RESET = "\27[0m"
        }

        local function process_thinking_sections(text)
            local result = ""
            local pos = 1
            while pos <= #text do
                local think_start = text:find("<think>", pos)
                if not think_start then
                    result = result .. text:sub(pos)
                    break
                end
                result = result .. text:sub(pos, think_start - 1)
                local think_end = text:find("</think>", think_start)
                if not think_end then
                    result = result .. COLORS.THINKING .. text:sub(think_start + 7) .. COLORS.RESET
                    break
                end
                local thinking_text = text:sub(think_start + 7, think_end - 1)
                result = result .. COLORS.THINKING .. thinking_text .. COLORS.RESET
                pos = think_end + 8
            end
            return result
        end

        local function wrap_text(text, max_width)
            text = process_thinking_sections(text)
            local lines = {}
            local current_line = ""
            local current_color = ""
            local function visible_length(str)
                return #(str:gsub("\27%[[%d;]+m", ""))
            end
            for word in text:gmatch("%S+") do
                local word_start_color = ""
                if word:match("^\27%[[%d;]+m") then
                    word_start_color = word:match("^(\27%[[%d;]+m)")
                    current_color = word_start_color
                end
                local potential_line = current_line == "" and word or current_line .. " " .. word
                if visible_length(potential_line) <= max_width then
                    current_line = potential_line
                else
                    table.insert(lines, current_line)
                    current_line = current_color .. word
                end
            end
            if #current_line > 0 then
                table.insert(lines, current_line)
            end
            return lines
        end

        return {
            width = width,
            height = height,
            input_text = "",
            cursor_visible = true,
            log_entries = {},

            add_log_entry = function(self, entry)
                local timestamp = time.now():format(time.TimeOnly)
                table.insert(self.log_entries, timestamp .. " - " .. entry)
                if #self.log_entries > 50 then
                    table.remove(self.log_entries, 1)
                end
            end,

            render = function(self, session)
                local total_height = self.height
                -- Layout:
                --  • Chat panel (header + chat area)
                --  • Memory panel (header + 2 lines)
                --  • Action Log panel (header + 3 lines)
                --  • Input section (border, input line, bottom border)
                local memory_area_height = 2
                local action_log_height = 3
                local fixed_lines = 1 + 1 + 1 + 1 + memory_area_height + 1 + 1 + action_log_height + 1 + 1 + 1
                local chat_area_height = total_height - fixed_lines

                local lines = {}

                -- Chat panel header
                table.insert(lines, "┌" .. string.rep("─", self.width - 2) .. "┐")
                local chat_title = " Chat Session "
                local pad_left = math.floor((self.width - 2 - #chat_title) / 2)
                local pad_right = self.width - 2 - #chat_title - pad_left
                table.insert(lines, "│" .. string.rep(" ", pad_left) .. chat_title .. string.rep(" ", pad_right) .. "│")
                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")

                -- Chat area
                local chat_lines = {}
                for _, msg in ipairs(session:get_history()) do
                    local prefix = msg.role == "user" and "You: " or "AI: "
                    local wrapped = wrap_text(prefix .. msg.content, self.width - 4)
                    for _, line in ipairs(wrapped) do
                        local vis = #(line:gsub("\27%[[%d;]+m", ""))
                        table.insert(chat_lines, "│ " .. line .. string.rep(" ", self.width - 3 - vis) .. "│")
                    end
                    table.insert(chat_lines, "│" .. string.rep(" ", self.width - 2) .. "│")
                end
                if #chat_lines > chat_area_height then
                    chat_lines = { unpack(chat_lines, #chat_lines - chat_area_height + 1, #chat_lines) }
                else
                    while #chat_lines < chat_area_height do
                        table.insert(chat_lines, "│" .. string.rep(" ", self.width - 2) .. "│")
                    end
                end
                for _, line in ipairs(chat_lines) do
                    table.insert(lines, line)
                end

                -- Memory panel
                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")
                local memory_title = " Memory "
                local pad_left_mem = math.floor((self.width - 2 - #memory_title) / 2)
                local pad_right_mem = self.width - 2 - #memory_title - pad_left_mem
                table.insert(lines, "│" .. string.rep(" ", pad_left_mem) .. memory_title .. string.rep(" ", pad_right_mem) .. "│")
                local mem_content = session.get_memory and session:get_memory() or ""
                local mem_lines = wrap_text(mem_content, self.width - 4)
                for i = 1, memory_area_height do
                    local line_text = mem_lines[i] or ""
                    local vis = #(line_text:gsub("\27%[[%d;]+m", ""))
                    table.insert(lines, "│ " .. line_text .. string.rep(" ", self.width - 3 - vis) .. "│")
                end

                -- Action Log panel
                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")
                local log_title = " Action Log "
                local pad_left_log = math.floor((self.width - 2 - #log_title) / 2)
                local pad_right_log = self.width - 2 - #log_title - pad_left_log
                table.insert(lines, "│" .. string.rep(" ", pad_left_log) .. log_title .. string.rep(" ", pad_right_log) .. "│")
                local log_lines = {}
                for _, entry in ipairs(self.log_entries) do
                    local wrapped = wrap_text(entry, self.width - 4)
                    for _, line in ipairs(wrapped) do
                        local vis = #(line:gsub("\27%[[%d;]+m", ""))
                        table.insert(log_lines, "│ " .. line .. string.rep(" ", self.width - 3 - vis) .. "│")
                    end
                end
                local start_log = math.max(1, #log_lines - action_log_height + 1)
                for i = start_log, #log_lines do
                    table.insert(lines, log_lines[i])
                end

                -- Input section
                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")
                local base_input = "│> " .. self.input_text
                local vis_len = #(base_input:gsub("\27%[[%d;]+m", ""))
                local cursor_char = self.cursor_visible and "▋" or ""
                local input_line = base_input .. cursor_char .. string.rep(" ", self.width - 2 - vis_len - (self.cursor_visible and 1 or 0)) .. "│"
                table.insert(lines, input_line)
                table.insert(lines, "└" .. string.rep("─", self.width - 2) .. "┘")

                return table.concat(lines, "\n")
            end
        }
    end
}

return M
