local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)
    local current_cursor = 0
    local selected_items = {}

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        }),
        up = btea.bind({
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        }),
        down = btea.bind({
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        }),
        select = btea.bind({
            keys = {"space"},
            help = {key = "space", desc = "select item"}
        }),
        toggle_filter = btea.bind({
            keys = {"/"},
            help = {key = "/", desc = "toggle filter"}
        })
    }

    local function limit_cursor(val, max)
        if val < 0 then return 0 end
        if val > max then return max end
        return val
    end

    local delegate = {
        height = function() return 2 end,
        spacing = function() return 1 end,

        render = function(self, model, index, item)
            current_cursor = limit_cursor(model:cursor(), 2)
            local is_selected = current_cursor == index
            local is_multi_selected = selected_items[index + 1]

            local cursor = is_selected and "→" or " "
            local checkbox = is_multi_selected and "[×]" or "[ ]"
            local status = item.is_active and "●" or "○"

            local title_style = btea.style()
            local desc_style = btea.style():foreground("#6C7086")

            if is_selected then
                title_style = title_style:foreground("#89B4FA"):bold()
                desc_style = desc_style:foreground("#89B4FA")
            end

            local title_line = string.format("%s %s %s [%d/%d] %s",
                cursor, checkbox, status, index, current_cursor,
                title_style:render(item.title))
            local desc_line = "    " .. desc_style:render(item.description)

            return title_line .. "\n" .. desc_line
        end,

        update = function(self, msg, model)
            if type(msg) == "table" and msg.type == "update" and msg.key then
                if app.keys.select:matches(msg) then
                    local cursor = model:cursor()
                    if cursor >= 0 then -- Ensure valid cursor
                        local idx = cursor + 1
                        selected_items[idx] = not selected_items[idx]
                        app.list:select(cursor)
                    end
                end
            end
        end
    }

    -- Create the list component
    app.list = btea.list({
        title = "List Demo",
        width = app.window.width - 4,
        height = app.window.height - 4,
        items = {
            {
                title = "Item 1",
                description = "First item",
                is_active = true,
                filter_value = "item 1"
            },
            {
                title = "Item 2",
                description = "Second item",
                is_active = false,
                filter_value = "item 2"
            },
            {
                title = "Item 3",
                description = "Third item",
                is_active = true,
                filter_value = "item 3"
            }
        },
        delegate = delegate,
        show_filter = true,
        filtering_enabled = true,
        styles = {
            title = btea.style()
                :foreground("#CDD6F4")
                :bold(),
            filter_prompt = btea.style()
                :foreground("#89B4FA"),
            filter_cursor = btea.style()
                :foreground("#F5C2E7"),
            container = btea.style()
                :border(btea.borders.ROUNDED)
                :padding(1)
                :background("#1E1E2E")
        },
        filter = function(term, targets)
            local results = {}
            for i, target in ipairs(targets) do
                if target:find(term, 1, true) then
                    table.insert(results, { index = i - 1, matches = { 1, #target } })
                end
            end
            return results
        end
    })

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.list:set_width(self.window.width - 4)
            self.list:set_height(self.window.height - 4)
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            end
        end

        -- Update list state
        local cmd = self.list:update(msg)
        if cmd then self:dispatch(cmd) end

        return false -- continue running
    end

    -- View function
    local function view(self)
        return self.list:view()
    end

    -- Run the app
    app:run(update, view)
end

return App