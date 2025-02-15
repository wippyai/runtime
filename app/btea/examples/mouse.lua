local bapp = require("bapp")
local time = require("time")

-- Button helper
local function create_button(zone_manager, id, label, styles)
    styles = styles or {
        normal = btea.style()
            :padding(1, 2)
            :border(btea.borders.ROUNDED)
            :background("#45475A")
            :foreground("#FFFFFF"),

        hover = btea.style()
            :padding(1, 2)
            :border(btea.borders.ROUNDED)
            :background("#FF79C6")
            :foreground("#000000"),

        active = btea.style()
            :padding(1, 2)
            :border(btea.borders.ROUNDED)
            :background("#89B4FA")
            :foreground("#1E1E2E")
    }

    return {
        id = id,
        label = label,
        styles = styles,
        is_hovered = false,
        is_active = false,

        check_hover = function(self, msg)
            if msg.mouse then
                local zone = zone_manager:get(self.id)
                if zone then
                    local was_hovered = self.is_hovered
                    self.is_hovered = zone:in_bounds(msg)
                    return was_hovered ~= self.is_hovered
                end
            end
            return false
        end,

        check_click = function(self, msg)
            if msg.mouse and msg.mouse.action == "press" then
                local zone = zone_manager:get(self.id)
                if zone then
                    return zone:in_bounds(msg)
                end
            end
            return false
        end,

        set_active = function(self, active)
            self.is_active = active
        end,

        render = function(self)
            local style = self.is_active and self.styles.active
                or self.is_hovered and self.styles.hover
                or self.styles.normal
            return zone_manager:mark(self.id, style:render(self.label))
        end
    }
end

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor,
        btea.commands.enable_mouse_all_motion
    }

    local app = bapp.new(init_commands)

    -- Create zone manager
    local zone_manager = btea.zone_manager()

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        })
    }

    -- App state
    app.state = {
        log_messages = {},
        auto_scroll = true
    }

    -- Create viewport
    app.viewport = btea.viewport({
        width = 60,
        height = 20,
        mouse_wheel_enabled = true,
        style = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E")
    })

    -- Create buttons
    app.buttons = {
        add = create_button(zone_manager, "add-msg", "Add Message"),
        scroll = create_button(zone_manager, "toggle-scroll", "Auto-scroll: ON"),
        clear = create_button(zone_manager, "clear", "Clear Logs")
    }

    -- Update auto-scroll button state
    local function update_scroll_button()
        app.buttons.scroll.label = "Auto-scroll: " .. (app.state.auto_scroll and "ON" or "OFF")
        app.buttons.scroll:set_active(app.state.auto_scroll)
    end
    update_scroll_button()

    -- Helper to add log message
    local function add_log(message)
        table.insert(app.state.log_messages, message)
        local content = table.concat(app.state.log_messages, "\n")
        app.viewport:set_content(content)

        if app.state.auto_scroll then
            app:dispatch(app.viewport:scroll_to_bottom())
        end
    end

    -- Add some initial messages
    for i = 1, 20 do
        add_log(string.format("Initial log message #%d", i))
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.viewport:set_width(math.min(60, self.window.width - 4))
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            end
        end

        -- Handle mouse events
        if type(msg) == "table" and msg.type == "update" and msg.mouse then
            -- Check hover states
            for _, btn in pairs(self.buttons) do
                btn:check_hover(msg)
            end

            -- Handle scrolling
            if self.viewport.mouse_wheel_enabled then
                if msg.mouse.button == "wheel_up" then
                    self:dispatch(self.viewport:line_up(3))
                elseif msg.mouse.button == "wheel_down" then
                    self:dispatch(self.viewport:line_down(3))
                end
            end

            -- Handle button clicks
            if msg.mouse.action == "press" then
                if self.buttons.add:check_click(msg) then
                    local now = time.now()
                    add_log(string.format("New message added at %s!", now:format("15:04:05")))
                elseif self.buttons.scroll:check_click(msg) then
                    self.state.auto_scroll = not self.state.auto_scroll
                    update_scroll_button()
                elseif self.buttons.clear:check_click(msg) then
                    self.state.log_messages = {}
                    self.viewport:set_content("")
                end
            end
        end

        -- Update viewport
        local cmd = self.viewport:update(msg)
        if cmd then self:dispatch(cmd) end

        return false -- continue running
    end

    -- View function
    local function view(self)
        -- Render buttons
        local add_btn = self.buttons.add:render()
        local scroll_btn = self.buttons.scroll:render()
        local clear_btn = self.buttons.clear:render()

        -- Help text style
        local help_style = btea.style()
            :foreground("#6C7086")
            :italic()

        -- Combine view elements
        local view = {
            btea.text.join_horizontal(btea.text.position.LEFT, add_btn, "  ", scroll_btn, "  ", clear_btn),
            "",
            self.viewport:view(),
            "",
            help_style:render("Mouse wheel to scroll | Click buttons or use q/^C to quit")
        }

        return zone_manager:scan(table.concat(view, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App