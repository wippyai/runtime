local bapp = require("bapp")
local time = require("time")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor,
        btea.commands.enable_mouse_all_motion
    }

    local app = bapp.new(init_commands)

    -- Create a viewport for our log viewer
    app.log_view = btea.viewport {
        width = app.window.width - 4,
        height = app.window.height - 2,
        mouse_wheel_enabled = true,
        style = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E")
    }

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        }),
        pagedown = btea.bind({
            keys = {"pgdown", " "},
            help = {key = "pgdn/space", desc = "page down"}
        }),
        pageup = btea.bind({
            keys = {"pgup", "b"},
            help = {key = "pgup/b", desc = "page up"}
        }),
        down = btea.bind({
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "down"}
        }),
        up = btea.bind({
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "up"}
        }),
        add = btea.bind({
            keys = {"a"},
            help = {key = "a", desc = "add log"}
        })
    }

    -- Styles for different log levels
    app.styles = {
        info = btea.style():foreground("#89B4FA"),
        warn = btea.style():foreground("#FAB387"),
        error = btea.style():foreground("#F38BA8"),
        debug = btea.style():foreground("#A6E3A1"),
        help = btea.style():foreground("#6C7086"):italic()
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

    -- Helper for viewport navigation
    local function scroll_viewport(self, cmd)
        if cmd then self:dispatch(cmd) end
    end

    -- Initialize with some logs
    app.logs = {}
    for i = 1, 50 do
        table.insert(app.logs, random_log())
    end
    app.log_view:set_content(table.concat(app.logs, "\n"))

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.log_view:set_width(self.window.width - 4)
            self.log_view:set_height(self.window.height - 2)
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.pagedown:matches(msg) then
                scroll_viewport(self, self.log_view:page_down())
            elseif self.keys.pageup:matches(msg) then
                scroll_viewport(self, self.log_view:page_up())
            elseif self.keys.down:matches(msg) then
                scroll_viewport(self, self.log_view:line_down())
            elseif self.keys.up:matches(msg) then
                scroll_viewport(self, self.log_view:line_up())
            elseif self.keys.add:matches(msg) then
                -- Add a new random log entry
                table.insert(self.logs, random_log())
                self.log_view:set_content(table.concat(self.logs, "\n"))
                -- If we were at the bottom, scroll to new content
                if self.log_view:at_bottom() then
                    scroll_viewport(self, self.log_view:scroll_to_bottom())
                end
            end
        end

        -- Handle mouse wheel events
        if type(msg) == "table" and msg.type == "update" and msg.mouse and self.log_view.mouse_wheel_enabled then
            if msg.mouse.button == "wheel_up" then
                scroll_viewport(self, self.log_view:line_up(3))
            elseif msg.mouse.button == "wheel_down" then
                scroll_viewport(self, self.log_view:line_down(3))
            end
        end

        -- Update viewport state
        scroll_viewport(self, self.log_view:update(msg))

        return false -- continue running
    end

    -- View function
    local function view(self)
        -- Get viewport content
        local viewport_view = self.log_view:view()

        -- Add help text below viewport
        local help_text = "↑/k up | ↓/j down | pgup/b page up | pgdn/space page down | a add log | ^C/q/esc quit"
        local help = self.styles.help:render(help_text)

        return viewport_view .. "\n" .. help
    end

    -- Run the app
    app:run(update, view)
end

return App