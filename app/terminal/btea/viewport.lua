local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Create a viewport for our log viewer
    app.log_view = btea.new_viewport {
        width = 60,
        height = 20,
        mouse_wheel_enabled = true,
        style = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E")
    }

    -- Setup key bindings using bapp
    app.keys = bapp.create_keys({
        pagedown = {
            keys = { "pgdown", " " },
            help = { key = "pgdn/space", desc = "page down" }
        },
        pageup = {
            keys = { "pgup", "b" },
            help = { key = "pgup/b", desc = "page up" }
        },
        down = {
            keys = { "down", "j" },
            help = { key = "↓/j", desc = "down" }
        },
        up = {
            keys = { "up", "k" },
            help = { key = "↑/k", desc = "up" }
        },
        add = {
            keys = { "a" },
            help = { key = "a", desc = "add log" }
        }
    })

    -- Styles for different log levels
    app.styles = {
        info = btea.style():foreground("#89B4FA"),
        warn = btea.style():foreground("#FAB387"),
        error = btea.style():foreground("#F38BA8"),
        debug = btea.new_style():foreground("#A6E3A1"),
        help = btea.new_style():foreground("#6C7086"):italic()
    }

    -- Sample log levels and messages for random generation
    local log_levels = { "info", "warn", "error", "debug" }
    local sample_messages = {
        "Application started",
        "Processing request",
        "Database connection established",
        "Cache miss detected",
        "Invalid input received",
        "Memory usage: 256MB",
        "Request timeout",
        "Task completed successfully",
        "Configuration loaded",
        "Rate limit exceeded"
    }

    -- Helper to generate a random log entry
    local function random_log()
        local level = log_levels[math.random(1, #log_levels)]
        local msg = sample_messages[math.random(1, #sample_messages)]
        local now = time.now()
        local timestamp = now:format("15:04:05")
        return app.styles[level]:render(string.format("[%s] [%s] %s", timestamp, level:upper(), msg))
    end

    -- Initialize with some logs
    app.logs = {}
    for i = 1, 50 do
        table.insert(app.logs, random_log())
    end
    app.log_view:set_content(table.concat(app.logs, "\n"))

    -- Enable mouse support
    app:enable_mouse()

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.pagedown:matches(msg) then
                local cmd = self.log_view:page_down()
                if cmd then self:dispatch(cmd) end
            elseif self.keys.pageup:matches(msg) then
                local cmd = self.log_view:page_up()
                if cmd then self:dispatch(cmd) end
            elseif self.keys.down:matches(msg) then
                local cmd = self.log_view:line_down()
                if cmd then self:dispatch(cmd) end
            elseif self.keys.up:matches(msg) then
                local cmd = self.log_view:line_up()
                if cmd then self:dispatch(cmd) end
            elseif self.keys.add:matches(msg) then
                -- Add a new random log entry
                table.insert(self.logs, random_log())
                self.log_view:set_content(table.concat(self.logs, "\n"))
                -- If we were at the bottom, scroll to new content
                if self.log_view:at_bottom() then
                    local cmd = self.log_view:scroll_to_bottom()
                    if cmd then self:dispatch(cmd) end
                end
            end
        end

        -- Handle mouse wheel events
        if msg.mouse and self.log_view.mouse_wheel_enabled then
            if msg.mouse.button == "wheel_up" then
                local cmd = self.log_view:line_up(3)
                if cmd then self:dispatch(cmd) end
            elseif msg.mouse.button == "wheel_down" then
                local cmd = self.log_view:line_down(3)
                if cmd then self:dispatch(cmd) end
            end
        end

        -- Update viewport state
        local cmd = self.log_view:update(msg)
        if cmd then self:dispatch(cmd) end

        return false
    end

    -- View function
    local function view(self)
        -- Get viewport content
        local viewport_view = self.log_view:view()

        -- Add help text below viewport
        local help = self.styles.help:render(
            "↑/k up | ↓/j down | pgup/b page up | pgdn/space page down | a add log | q/^C quit"
        )

        return viewport_view .. "\n" .. help
    end

    -- Run the app
    app:run(update, view)
end

return App
