local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Setup key bindings using bapp
    app.keys = bapp.create_keys({
        toggle_help = {
            keys = { "?" },
            help = { key = "?", desc = "toggle help" }
        },
        nav_up = {
            keys = { "up", "k" },
            help = { key = "↑/k", desc = "move up" }
        },
        nav_down = {
            keys = { "down", "j" },
            help = { key = "↓/j", desc = "move down" }
        },
        action1 = {
            keys = { "1" },
            help = { key = "1", desc = "action one" }
        },
        action2 = {
            keys = { "2" },
            help = { key = "2", desc = "action two" }
        },
        save = {
            keys = { "ctrl+s" },
            help = { key = "^S", desc = "save" }
        },
        reload = {
            keys = { "ctrl+r" },
            help = { key = "^R", desc = "reload" }
        }
    })

    -- Create help component
    app.help = btea.new_help({
        width = 60,
        styles = {
            short_key = btea.new_style():foreground("#89B4FA"):bold(),
            short_desc = btea.new_style():foreground("#CDD6F4"),
            short_separator = btea.new_style():foreground("#45475A"),
            full_key = btea.new_style():foreground("#89B4FA"):bold(),
            full_desc = btea.new_style():foreground("#CDD6F4"),
            full_separator = btea.new_style():foreground("#45475A")
        }
    })

    -- Define our help keymap using a table
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
        base = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.new_style()
            :foreground("#CBA6F7")
            :bold(),

        content = btea.new_style()
            :foreground("#CDD6F4"),

        action = btea.new_style()
            :foreground("#F9E2AF")
            :italic()
    }

    -- Track last action
    app.last_action = "No action taken yet"

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                self.last_action = "Quitting..."
                return true
            elseif self.keys.toggle_help:matches(msg) then
                self.keymap.show_full = not self.keymap.show_full
                self.last_action = "Toggled help view"
            elseif self.keys.nav_up:matches(msg) then
                self.last_action = "Moved up"
            elseif self.keys.nav_down:matches(msg) then
                self.last_action = "Moved down"
            elseif self.keys.action1:matches(msg) then
                self.last_action = "Performed action one"
            elseif self.keys.action2:matches(msg) then
                self.last_action = "Performed action two"
            elseif self.keys.save:matches(msg) then
                self.last_action = "Saved"
            elseif self.keys.reload:matches(msg) then
                self.last_action = "Reloaded"
            end
        end
        return false
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

        -- Add help view
        self.help:set_show_all(self.keymap.show_full)
        table.insert(lines, self.help:view(self.keymap))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App
