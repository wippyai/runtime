local M = {}

M.LLMClient = {
    new = function(model, endpoint)
        return {
            model = model or "mistral:latest",
            endpoint = endpoint or "http://100.70.10.9:11434/api/chat", -- Changed to chat endpoint

            query = function(self, prompt, history, update_channel)
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
                -- Update the current message's content
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
        -- ANSI escape codes for colors
        local COLORS = {
            THINKING = "\27[33m", -- Yellow for thinking
            RESET = "\27[0m"
        }

        -- Helper function to process thinking sections
        local function process_thinking_sections(text)
            local result = ""
            local pos = 1

            while pos <= #text do
                local think_start = text:find("<think>", pos)
                if not think_start then
                    -- No more thinking sections, add remaining text
                    result = result .. text:sub(pos)
                    break
                end

                -- Add text before thinking section
                result = result .. text:sub(pos, think_start - 1)

                -- Find end of thinking section
                local think_end = text:find("</think>", think_start)
                if not think_end then
                    -- Unclosed thinking tag, treat rest as thinking
                    result = result .. COLORS.THINKING .. text:sub(think_start + 7) .. COLORS.RESET
                    break
                end

                -- Add thinking section with color
                local thinking_text = text:sub(think_start + 7, think_end - 1)
                result = result .. COLORS.THINKING .. thinking_text .. COLORS.RESET

                pos = think_end + 8 -- Move past </think>
            end

            return result
        end

        local function wrap_text(text, max_width)
            -- First process any thinking sections
            text = process_thinking_sections(text)

            local lines = {}
            local current_line = ""
            local current_color = ""

            -- Helper to calculate visible length (excluding ANSI sequences)
            local function visible_length(str)
                return #(str:gsub("\27%[[%d;]+m", ""))
            end

            for word in text:gmatch("%S+") do
                -- Preserve color codes at word boundaries
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
                    -- Start new line with current color state
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

            render = function(self, session)
                local lines = {
                    "┌" .. string.rep("─", self.width - 2) .. "┐",
                    "│" ..
                    string.rep(" ", math.floor((self.width - 14) / 2)) ..
                    "Chat Session" .. string.rep(" ", self.width - 14 - math.floor((self.width - 14) / 2)) .. "│",
                    "│" .. string.rep("─", self.width - 2) .. "│"
                }

                -- Display all messages
                for _, msg in ipairs(session:get_history()) do
                    local prefix = msg.role == "user" and "You: " or "AI: "
                    local wrapped = wrap_text(prefix .. msg.content, self.width - 4)
                    for _, line in ipairs(wrapped) do
                        -- Calculate padding taking into account ANSI escape sequences
                        local visible_length = line:gsub("\27%[[%d;]+m", "")
                        local padding = self.width - 3 - #visible_length
                        table.insert(lines, "│ " .. line .. COLORS.RESET .. string.rep(" ", padding) .. "│")
                    end
                    table.insert(lines, "│" .. string.rep(" ", self.width - 2) .. "│")
                end

                while #lines < self.height - 3 do
                    table.insert(lines, "│" .. string.rep(" ", self.width - 2) .. "│")
                end

                table.insert(lines, "│" .. string.rep("─", self.width - 2) .. "│")
                local input_line = "│> " .. self.input_text
                if self.cursor_visible then
                    input_line = input_line .. "▋"
                end
                input_line = input_line .. string.rep(" ", self.width - 3 - #self.input_text) .. "│"
                table.insert(lines, input_line)
                table.insert(lines, "└" .. string.rep("─", self.width - 2) .. "┘")

                return table.concat(lines, "\n")
            end
        }
    end
}

return M
