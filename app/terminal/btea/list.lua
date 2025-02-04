local bapp = require("bapp")

function App()
    local app = bapp.new()
    local current_cursor = 0
    local selected_items = {}

    app.keys = bapp.create_keys({
        quit = { keys = { "q", "ctrl+c" } },
        up = { keys = { "up", "k" } },
        down = { keys = { "down", "j" } },
        select = { keys = { "space", " " } },
        toggle_filter = { keys = { "/" } }
    })

    local function limit_cursor(val, max)
        if val < 0 then return 0 end
        if val > max then return max end
        return val
    end

    local delegate = {
        height = function() return 2 end,
        spacing = function() return 1 end,

        render = function(model, index, item)
            current_cursor = limit_cursor(model:cursor(), 2)
            local is_selected = current_cursor == index
            local is_multi_selected = selected_items[index + 1]

            local cursor = is_selected and "→" or " "
            local checkbox = is_multi_selected and "[×]" or "[ ]"
            local status = item.is_active and "●" or "○"

            local title_style = btea.new_style()
            local desc_style = btea.new_style():foreground("#666666")

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

        update = function(msg, model)
            if msg.key and app.keys.select:matches(msg) then
                local cursor = model:cursor()
                if cursor >= 0 then  -- Ensure valid cursor
                    local idx = cursor + 1
                    selected_items[idx] = not selected_items[idx]
                    app.list:select(cursor)
                end
            end
            return nil
        end
    }

    app.list = btea.new_list({
        title = "List Demo",
        width = app.window.width,
        height = app.window.height,
        items = {
            {
                title = "Item 1",
                description = "First item",
                is_active = true
            },
            {
                title = "Item 2",
                description = "Second item",
                is_active = false
            },
            {
                title = "Item 3",
                description = "Third item",
                is_active = true
            }
        },
        delegate = delegate,
        show_filter = true,
        filtering_enabled = true,
        styles = {
            title = btea.new_style():foreground("#CDD6F4"):bold(),
            filter_prompt = btea.new_style():foreground("#89B4FA"),
            filter_cursor = btea.new_style():foreground("#F5C2E7")
        }
    })

    local function update(self, msg)
        if msg.key then
            self:dispatch(self.list:update(msg))
        end
        return false
    end

    app:run(update, function(self) return self.list:view() end)
    return app
end

return App