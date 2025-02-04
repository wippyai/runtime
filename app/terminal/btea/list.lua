local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Setup key bindings
    app.keys = bapp.create_keys({
        quit = { keys = { "q", "ctrl+c" } },
        up = { keys = { "up", "k" } },
        down = { keys = { "down", "j" } },
        select = { keys = { " " } },
        toggle_filter = { keys = { "/" } }
    })

    -- Track selected items
    local selected_items = {}

    -- Create items list
    local items = {
        {
            title = "Item 1",
            description = "First item description",
            is_active = true,
            filter_value = function(self)
            print(self.title)
                return self.title .. " " .. self.description
            end
        },
        {
            title = "Item 2",
            description = "Second item description",
            is_active = false,
            filter_value = function(self)
                return self.title .. " " .. self.description
            end
        },
        {
            title = "Item 3",
            description = "Third item description",
            is_active = true,
            filter_value = function(self)
                return self.title .. " " .. self.description
            end
        }
    }

    -- Custom delegate implementation
    local delegate = {
        height = function()
            return 2
        end,

        spacing = function()
            return 1
        end,

        render = function(model, index, item)
            local is_selected = model:cursor() == index
            local is_multi_selected = selected_items[index] ~= nil

            -- Build indicators
            local cursor = is_selected and "→" or " "
            local checkbox = is_multi_selected and "[×]" or "[ ]"
            local status = item.is_active and "●" or "○"

            -- Style setup
            local title_style = btea.new_style()
            local desc_style = btea.new_style():foreground("#666666")

            if is_selected then
                title_style = title_style:foreground("#89B4FA"):bold()
                desc_style = desc_style:foreground("#89B4FA")
            end

            if is_multi_selected then
                title_style = title_style:italic()
            end

            -- Render lines
            local title_line = string.format("%s %s %s %s",
                cursor, checkbox, status, title_style:render(item.title))
            local desc_line = "    " .. desc_style:render(item.description)

            return title_line .. "\n" .. desc_line
        end,

        update = function(msg, model)
            -- Handle space key for multi-selection
            if msg.key and app.keys.select:matches(msg) then
                local cursor = model:cursor()
                if cursor >= 0 then
                    if selected_items[cursor + 1] then
                        selected_items[cursor + 1] = nil
                    else
                        selected_items[cursor + 1] = true
                    end
                end
            end
            return nil
        end
    }

    -- Create list with our items
    app.list = btea.new_list({
        title = "Interactive List Demo",
        width = app.window.width,
        height = app.window.height,
        items = items,
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
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.up:matches(msg) then
                self.list:cursor_up()
            elseif self.keys.down:matches(msg) then
                self.list:cursor_down()
            end

           self:dispatch(self.list:update(msg))
        end
        return false
    end

    local function view(self)
        return self.list:view()
    end

    app:run(update, view)
    return app
end

return App