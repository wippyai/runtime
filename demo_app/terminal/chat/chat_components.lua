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
                        if log_fn then log_fn("LLMClient error: " .. tostring(err)) end
                        update_channel:send({ type = "error", error = err })
                        return
                    end

                    local stream = response and response.stream
                    if stream then
                        local buffer = ""
                        while true do
                            local chunk, err = stream:read()
                            if err or not chunk then break end

                            local success, decoded = pcall(json.decode, chunk)
                            if success and decoded and decoded.message and decoded.message.content then
                                buffer = buffer .. decoded.message.content
                                -- Send buffer when we hit sentence boundary or buffer gets large
                                if buffer:match("[%.%!%?]%s*$") or #buffer > 50 then
                                    update_channel:send({ type = "update", text = buffer })
                                    buffer = ""
                                    time.sleep("30ms")
                                end
                            end
                        end
                        -- Send any remaining buffered content
                        if #buffer > 0 then
                            update_channel:send({ type = "update", text = buffer })
                        end
                        stream:close()
                        update_channel:send({ type = "done" })
                        if log_fn then log_fn("LLMClient: query finished") end
                    end
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

            add_message = function(self, role, content)
                local msg = { role = role, content = content }
                table.insert(self.messages, msg)
                return msg
            end,

            start_response = function(self)
                self.is_responding = true
                self.current_response = ""
                -- Create a placeholder message that we'll update
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
            end
        }
    end
}

M.ChatUI = {
    new = function(width, height)
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
            log_entries = {}, -- new property for action log

            add_log_entry = function(self, entry)
                table.insert(self.log_entries, os.date("%H:%M:%S") .. " - " .. entry)
                -- Limit log history to the most recent 50 entries
                if #self.log_entries > 50 then
                    table.remove(self.log_entries, 1)
                end
            end,

            render = function(self, session)
                local total_height = self.height
                local log_panel_height = 3 -- fixed height for log panel
                local fixed_lines = 11     -- borders, headers, input etc.
                local chat_area_height = total_height - fixed_lines

                local lines = {}

                -- Top border and title for chat panel
                table.insert(lines, "┌" .. string.rep("─", self.width - 2) .. "┐")
                local title = " Chat Session "
                local pad_left = math.floor((self.width - 2 - #title) / 2)
                local pad_right = self.width - 2 - #title - pad_left
                table.insert(lines, "│" .. string.rep(" ", pad_left) .. title .. string.rep(" ", pad_right) .. "│")
                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")

                -- Prepare chat messages (wrap and limit to chat_area_height lines)
                local chat_lines = {}
                for _, msg in ipairs(session:get_history()) do
                    local prefix = msg.role == "user" and "You: " or "AI: "
                    local wrapped = wrap_text(prefix .. msg.content, self.width - 4)
                    for _, line in ipairs(wrapped) do
                        table.insert(chat_lines,
                            "│ " .. line .. string.rep(" ", self.width - 3 - #(line:gsub("\27%[[%d;]+m", ""))) .. "│")
                    end
                    table.insert(chat_lines, "│" .. string.rep(" ", self.width - 2) .. "│")
                end
                -- Show only the last chat_area_height lines
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

                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")
                -- Log panel header
                local log_title = " Action Log "
                local pad_left_log = math.floor((self.width - 2 - #log_title) / 2)
                local pad_right_log = self.width - 2 - #log_title - pad_left_log
                table.insert(lines,
                    "│" .. string.rep(" ", pad_left_log) .. log_title .. string.rep(" ", pad_right_log) .. "│")

                -- Render log panel (show last log_panel_height entries)
                local log_lines = {}
                for _, entry in ipairs(self.log_entries) do
                    local wrapped = wrap_text(entry, self.width - 4)
                    for _, line in ipairs(wrapped) do
                        table.insert(log_lines,
                            "│ " .. line .. string.rep(" ", self.width - 3 - #(line:gsub("\27%[[%d;]+m", ""))) .. "│")
                    end
                end
                local start_log = math.max(1, #log_lines - log_panel_height + 1)
                for i = start_log, #log_lines do
                    table.insert(lines, log_lines[i])
                end
                while #lines < total_height - 2 do
                    table.insert(lines, "│" .. string.rep(" ", self.width - 2) .. "│")
                end

                -- Input line
                local input_line = "│> " .. self.input_text
                if self.cursor_visible then
                    input_line = input_line .. "▋"
                end
                local visible_len = #(input_line:gsub("\27%[[%d;]+m", ""))
                input_line = input_line .. string.rep(" ", self.width - 2 - visible_len) .. "│"
                table.insert(lines, "├" .. string.rep("─", self.width - 2) .. "┤")
                table.insert(lines, input_line)
                table.insert(lines, "└" .. string.rep("─", self.width - 2) .. "┘")

                return table.concat(lines, "\n")
            end
        }
    end
}

return M
