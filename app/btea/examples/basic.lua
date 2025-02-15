local time = require("time")
local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Initialize app state
    app.messages = {}

    -- Initialize text input
    app.input = btea.text_input({
        placeholder = "Type something...",
        width = app.window.width - 8
    })
    app.input:focus()

    -- Define styles
    app.styles = {
        box = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1)
            :underline(),

        -- Messages styling
        key = btea.style():foreground("#F9E2AF"):bold(),
        mouse = btea.style():foreground("#94E2D5"):bold(),
        size = btea.style():foreground("#A6E3A1"):bold(),
        tick = btea.style():foreground("#89B4FA"),
        timestamp = btea.style():foreground("#6C7086"),
        command = btea.style():foreground("#F38BA8"):italic(),

        -- Input field styling
        input = btea.style()
            :foreground("#F5C2E7")
            :background("#313244")
            :padding(0, 1)
            :margin(1, 0)
    }

    -- Create a done channel for cleanup coordination
    local done = channel.new()

    -- Message helper functions
    local function format_msg(msg)
        if not msg then return app.styles.command:render("INVALID MSG") end

        -- Handle raw tick strings
        if type(msg) == "string" and msg == "tick" then
            return app.styles.tick:render("TICK")
        end

        -- Handle update messages
        if type(msg) == "table" and msg.type == "update" then
            if msg.key then
                return app.styles.key:render(string.format(
                    "KEY %s [%s]",
                    msg.key.String or "",
                    msg.key.key_type or "unknown"
                ))
            elseif msg.mouse then
                return app.styles.mouse:render(string.format(
                    "MOUSE %s at %d,%d",
                    msg.mouse.action or "unknown",
                    msg.mouse.x or 0,
                    msg.mouse.y or 0
                ))
            elseif msg.window_size then
                return app.styles.size:render(string.format(
                    "SIZE %d×%d",
                    msg.window_size.width or 80,
                    msg.window_size.height or 24
                ))
            end
        end

        return app.styles.command:render("UNKNOWN MSG: " .. (msg.type or "nil"))
    end

    -- View rendering
    local function view(self)
        local content_width = self.window.width - 6
        local header_divider = string.rep("─", content_width)

        local display_content = {
            self.styles.header:render("BubbleTea Debug View"),
            self.styles.timestamp:render(header_divider),
            self.styles.header:render("Message Log:"),
            ""
        }

        -- Calculate visible messages area
        local max_visible = self.window.height - 8
        local start_idx = math.max(1, #self.messages - max_visible)

        -- Add messages with improved formatting
        for i = start_idx, #self.messages do
            local line = self.messages[i]
            if #line > content_width then
                line = line:sub(1, content_width - 3) .. "..."
            end
            table.insert(display_content, " " .. line)
        end

        -- Add input field with better visual separation
        table.insert(display_content, "")
        table.insert(display_content, self.styles.input:render(self.input:view() or ""))

        return self.styles.box
            :width(self.window.width - 2)
            :height(self.window.height - 2)
            :render(table.concat(display_content, "\n"))
    end

    -- Update handler
    local function update(self, msg)
        -- Update window size if needed
        if msg.window_size and type(msg.window_size) == "table" then
            self.input:set_width(self.window.width - 10)
        end

        -- Update text input
        local cmd = self.input:update(msg)
        if cmd then
            self:dispatch(cmd)
        end

        -- Format and log the message with timestamp
        local now = time.now()
        local formatted = string.format("%s %s",
            self.styles.timestamp:render(now:format("15:04:05")),
            format_msg(msg)
        )

        -- Handle input submission
        if msg.key and msg.key.key_type == "enter" then
            local value = self.input:value()
            if value ~= "" then
                table.insert(self.messages,
                    self.styles.command:render("INPUT: " .. value)
                )
                self.input:set_value("")
            end
        end

        table.insert(self.messages, formatted)

        -- Check for quit conditions
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if msg.key.key_type == "ctrl+c" or msg.key.String == "esc" then
                return true -- signal quit
            end
        end

        return false -- continue running
    end

    -- Start ticker in background
    coroutine.spawn(function()
        local ticker = time.ticker("1s")
        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                ticker:stop()
                break
            end

            upstream.send({ type = "update", tick = true })
        end
    end)

    -- Initialize and run the app
    app:init_terminal()
    app:run(update, view)

    -- Cleanup
    done:close()
end

return App