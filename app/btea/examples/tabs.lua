local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Define colors
    local colors = {
        highlight = "#7D56F4",
        active_fg = "#89B4FA",
        inactive_fg = "#6C7086",
        bg = "#1E1E2E"
    }

    -- Create base styles
    local tab_base = btea.style()
        :padding(0, 1)
        :margin(0, 1)
        :border(btea.borders.ROUNDED)
        :background(colors.bg)

    local active_tab = tab_base:copy()
        :foreground(colors.active_fg)
        :bold(true)

    local inactive_tab = tab_base:copy()
        :foreground(colors.inactive_fg)

    -- Window style
    local window_style = btea.style()
        :padding(1)
        :margin(0, 1)
        :border(btea.borders.NORMAL)
        :border_foreground(colors.highlight)
        :background(colors.bg)

    -- Container style
    local container_style = btea.style()
        :padding(1)

    -- Initialize tabs state
    app.tabs = {
        titles = { "Home", "Profile", "Settings", "Help" },
        content = {
            "Welcome to the Home tab!\nThis is a demo of the tab system.",
            "This is your Profile tab.\nHere you can see your information.",
            "Configure your Settings here.\nLots of options to choose from!",
            "Need Help? You're in the right place!\nCheck out our documentation."
        },
        active = 1,
        window_size = { width = app.window.width, height = app.window.height }
    }

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "esc"},
            help = {key = "^C/esc", desc = "quit application"}
        }),
        next_tab = btea.bind({
            keys = {"right", "l", "tab"},
            help = {key = "→/l/tab", desc = "next tab"}
        }),
        prev_tab = btea.bind({
            keys = {"left", "h", "shift+tab"},
            help = {key = "←/h/shift+tab", desc = "previous tab"}
        }),
        first_tab = btea.bind({
            keys = {"home", "1"},
            help = {key = "home/1", desc = "first tab"}
        }),
        last_tab = btea.bind({
            keys = {"end", "4"},
            help = {key = "end/4", desc = "last tab"}
        })
    }

    -- Tab navigation functions
    local function next_tab(self)
        self.tabs.active = (self.tabs.active % #self.tabs.titles) + 1
    end

    local function prev_tab(self)
        self.tabs.active = ((self.tabs.active - 2) % #self.tabs.titles) + 1
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.tabs.window_size = msg.window_size
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.next_tab:matches(msg) then
                next_tab(self)
            elseif self.keys.prev_tab:matches(msg) then
                prev_tab(self)
            elseif self.keys.first_tab:matches(msg) then
                self.tabs.active = 1
            elseif self.keys.last_tab:matches(msg) then
                self.tabs.active = #self.tabs.titles
            end
        end

        return false -- continue running
    end

    -- View function
    local function view(self)
        -- Render tabs
        local rendered_tabs = {}
        for i, title in ipairs(self.tabs.titles) do
            local style = i == self.tabs.active and active_tab or inactive_tab
            table.insert(rendered_tabs, style:render(" " .. title .. " "))
        end

        -- Join tabs with spacing
        local tab_bar = btea.text.join_horizontal(btea.text.position.TOP, unpack(rendered_tabs))

        -- Create content window
        local window = window_style
            :width(self.tabs.window_size.width - 4)
            :render(self.tabs.content[self.tabs.active])

        -- Combine everything
        return container_style:render(tab_bar .. "\n" .. window)
    end

    -- Run the app
    app:run(update, view)
end

return App