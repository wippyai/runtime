local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        }),
        toggle_help = btea.bind({
            keys = {"?"},
            help = {key = "?", desc = "toggle help"}
        }),
        nav_up = btea.bind({
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        }),
        nav_down = btea.bind({
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        }),
        action1 = btea.bind({
            keys = {"1"},
            help = {key = "1", desc = "action one"}
        }),
        action2 = btea.bind({
            keys = {"2"},
            help = {key = "2", desc = "action two"}
        }),
        save = btea.bind({
            keys = {"ctrl+s"},
            help = {key = "^S", desc = "save"}
        }),
        reload = btea.bind({
            keys = {"ctrl+r"},
            help = {key = "^R", desc = "reload"}
        })
    }

    -- Create help component
    app.help = btea.help({
        width = app.window.width - 4,
        styles = {
            short_key = btea.style():foreground("#89B4FA"):bold(),
            short_desc = btea.style():foreground("#CDD6F4"),
            short_separator = btea.style():foreground("#45475A"),
            full_key = btea.style():foreground("#89B4FA"):bold(),
            full_desc = btea.style():foreground("#CDD6F4"),
            full_separator = btea.style():foreground("#45475A")
        }
    })

    -- Define help keymap
    app.keymap = {
        show_full = false,

        -- Return most important bindings for short help
        short_help = function(self)
            return {
                app.keys.quit,
                app.keys.toggle_help,
                app.keys.nav_up,
                app.keys.nav_down
            }
        end,

        -- Return all bindings grouped by category for full help
        full_help = function(self)
            return {
                -- Navigation group
                {
                    app.keys.nav_up,
                    app.keys.nav_down
                },
                -- Actions group
                {
                    app.keys.action1,
                    app.keys.action2
                },
                -- File operations group
                {
                    app.keys.save,
                    app.keys.reload
                },
                -- System group
                {
                    app.keys.toggle_help,
                    app.keys.quit
                }
            }
        end
    }

    -- Styles for demo layout
    app.styles = {
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.style()
            :foreground("#CBA6F7")
            :bold(),

        content = btea.style()
            :foreground("#CDD6F4"),

        action = btea.style()
            :foreground("#F9E2AF")
            :italic()
    }

    -- Track last action
    app.last_action = "No action taken yet"

    -- Helper to update last action
    local function set_action(self, action)
        self.last_action = action
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.help:set_width(self.window.width - 4)
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                set_action(self, "Quitting...")
                return true -- signal quit
            elseif self.keys.toggle_help:matches(msg) then
                self.keymap.show_full = not self.keymap.show_full
                set_action(self, "Toggled help view")
            elseif self.keys.nav_up:matches(msg) then
                set_action(self, "Moved up")
            elseif self.keys.nav_down:matches(msg) then
                set_action(self, "Moved down")
            elseif self.keys.action1:matches(msg) then
                set_action(self, "Performed action one")
            elseif self.keys.action2:matches(msg) then
                set_action(self, "Performed action two")
            elseif self.keys.save:matches(msg) then
                set_action(self, "Saved")
            elseif self.keys.reload:matches(msg) then
                set_action(self, "Reloaded")
            end
        end

        return false -- continue running
    end

    -- View function
    local function view(self)
        local lines = {
            self.styles.title:render("Help Table Demo"),
            "",
            self.styles.content:render("This demo shows how to use help with a Lua table."),
            self.styles.content:render("Press '?' to toggle between short and full help views."),
            "",
            self.styles.action:render("Last action: " .. self.last_action),
            ""
        }

        -- Add help view with current show_full state
        self.help:set_show_all(self.keymap.show_full)
        table.insert(lines, self.help:view(self.keymap))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App