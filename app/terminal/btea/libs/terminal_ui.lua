local M = {}

M.TerminalUI = {
    new = function(title, options)
        local window_width = 60
        local window_height = 12
        local log_messages = {}
        local max_log_messages = options and options.max_messages or 1000
        local done = channel.new()

        -- Create default styles
        local styles = {
            box = btea.style()
                :border(btea.borders.ROUNDED)
                :padding(1, 2)
                :foreground("#89B4FA")
                :background("#1E1E2E"),

            header = btea.style()
                :bold()
                :foreground("#CBA6F7")
                :padding(0, 1),

            error = btea.style()
                :foreground("#F38BA8")
                :bold(),

            success = btea.style()
                :foreground("#A6E3A1")
                :bold(),

            info = btea.new_style()
                :foreground("#89B4FA"),

            warning = btea.new_style()
                :foreground("#F9E2AF"),

            debug = btea.new_style()
                :foreground("#7F849C"),

            timestamp = btea.new_style()
                :foreground("#6C7086"),

            status = btea.new_style()
                :background("#313244")
                :padding(0, 1)
                :margin(1, 0)
        }

        local function run_ui_ticker()
            local ticker = time.ticker("1s")
            while true do
                local result = channel.select {
                    ticker:channel():case_receive(),
                    done:case_receive()
                }
                if result.channel == done then break end
                upstream.send("tick")
            end
        end

        return {
            -- Public methods
            start = function(self)
                coroutine.spawn(run_ui_ticker)
                return self
            end,

            log = function(self, level, message, data)
                local timestamp = time.now():format("15:04:05")
                local style = styles[level] or styles.info
                local formatted_message = string.format(
                    "%s %s: %s",
                    styles.timestamp:render(timestamp),
                    style:render(level:upper()),
                    message
                )

                if data then
                    local success, json_data = pcall(json.encode, data)
                    if success then
                        formatted_message = formatted_message .. " " ..
                            styles.info:render(json_data)
                    end
                end

                table.insert(log_messages, formatted_message)
                if #log_messages > max_log_messages then
                    table.remove(log_messages, 1)
                end

                upstream.send("tick")
                return self
            end,

            create_status_bar = function(self, status_data)
                local status_parts = {}
                for key, value in pairs(status_data) do
                    table.insert(status_parts, string.format("%s: %s", key, tostring(value)))
                end

                return styles.status:render(table.concat(status_parts, " | "))
            end,

            create_view = function(self, title, status_data)
                local content_width = window_width - 6
                local header_divider = string.rep("─", content_width)

                local display_content = {
                    styles.header:render(title),
                    styles.timestamp:render(header_divider)
                }

                local max_visible = window_height - 8
                local start_idx = math.max(1, #log_messages - max_visible)

                for i = start_idx, #log_messages do
                    local line = log_messages[i]
                    --if #line > content_width then
                    --    line = line:sub(1, content_width - 3) .. "..."
                    --end
                    table.insert(display_content, line)
                end

                table.insert(display_content, "")
                table.insert(display_content, self:create_status_bar(status_data))

                return styles.box
                    :width(window_width - 2)
                    :height(window_height - 2)
                    :render(table.concat(display_content, "\n"))
            end,

            handle_input = function(self, msg)
                if type(msg) == "table" and msg.type == "update" and msg.window_size then
                    window_width = msg.window_size.width
                    window_height = msg.window_size.height
                end
            end,

            stop = function(self)
                done:close()
            end,

            -- Getters
            get_logs = function(self)
                return log_messages
            end,

            get_window_size = function(self)
                return window_width, window_height
            end,

            clear_logs = function(self)
                log_messages = {}
                upstream.send("tick")
                return self
            end,

            -- Helper methods for formatted logging
            debug = function(self, msg, data)
                return self:log("debug", msg, data)
            end,

            info = function(self, msg, data)
                return self:log("info", msg, data)
            end,

            warn = function(self, msg, data)
                return self:log("warning", msg, data)
            end,

            error = function(self, msg, data)
                return self:log("error", msg, data)
            end,

            success = function(self, msg, data)
                return self:log("success", msg, data)
            end
        }
    end
}

return M