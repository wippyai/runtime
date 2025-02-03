local bapp = require("base_app")

function App()
    local app = bapp.new()

    -- Define colors
    local colors = {
        highlight = "#7D56F4",
        active_fg = "#89B4FA",
        inactive_fg = "#6C7086",
        bg = "#1E1E2E"
    }

    -- Create base styles
    local tab_base = btea.new_style()
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
    local window_style = btea.new_style()
        :padding(1)
        :margin(0, 1)
        :border(btea.borders.NORMAL)
        :border_foreground(colors.highlight)
        :background(colors.bg)

    -- Container style
    local container_style = btea.new_style()
        :padding(1)

    -- Initialize tabs
    app.tabs = bapp.create_tabs({
        titles = { "Home", "Profile", "Settings", "Help" },
        content = {
            "Welcome to the Home tab!\nThis is a demo of the tab system.",
            "This is your Profile tab.\nHere you can see your information.",
            "Configure your Settings here.\nLots of options to choose from!",
            "Need Help? You're in the right place!\nCheck out our documentation."
        }
    })

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true -- quit
            end
            self.tabs:handle_key(msg)
        end
        return false
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

    -- Setup key bindings
    app.keys = bapp.create_keys({})

    -- Run the app
    app:run(update, view)
end

return App
