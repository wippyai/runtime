local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Create some component items (spinner, progress setup same as before)
    local spinner = btea.new_spinner {
        type = btea.spinners.LINE,
        interval = 10
    }
    spinner:style(btea.new_style():foreground("#89B4FA"))
    app:dispatch(spinner:tick())

    local progress = btea.new_progress {
        width = 30,
        fill_type = "gradient",
        gradient = {
            from = "#F5C2E7",
            to = "#94E2D5"
        }
    }
    app:dispatch(progress:set_percent(0.7))

    local paginator = btea.new_paginator({
        type = btea.paginator_types.DOTS,
        per_page = 3,
        active_dot = "●",
        inactive_dot = "○"
    })
    paginator:set_total_pages(5)

    -- Setup key bindings including left/right for paginator
    app.keys = bapp.create_keys({
        quit = { keys = { "q", "ctrl+c" } },
        up = { keys = { "up", "k" } },
        down = { keys = { "down", "j" } },
        left = { keys = { "left", "h" } },
        right = { keys = { "right", "l" } }
    })

    -- Create mixed items list
    local items = {
        -- Simple text item
        {
            title = "Plain Text Item",
            content = "Just a regular text line",
            handle_key = function(self, msg, keys)
                -- Regular items don't handle keys
                return nil
            end
        },
        -- Spinner component item
        {
            title = "Loading Status",
            component = spinner,
            content = "Processing...",
            handle_key = function(self, msg, keys)
                -- Spinner doesn't need key handling
                return nil
            end
        },
        -- Progress bar item
        {
            title = "Download Progress",
            component = progress,
            content = "Downloading files...",
            handle_key = function(self, msg, keys)
                if keys.right:matches(msg) then
                    local new_percent = math.min(1.0, progress:percent() + 0.1)
                    return progress:set_percent(new_percent)
                elseif keys.left:matches(msg) then
                    local new_percent = math.max(0.0, progress:percent() - 0.1)
                    return progress:set_percent(new_percent)
                end
                return nil
            end
        },
        -- Paginator item
        {
            title = "Page Navigation",
            component = paginator,
            content = "Browse pages 1-5",
            handle_key = function(self, msg, keys)
                if keys.left:matches(msg) then
                    paginator:prev_page()
                    return nil  -- paginator doesn't return commands
                elseif keys.right:matches(msg) then
                    paginator:next_page()
                    return nil
                end
                return nil
            end
        },
    }

    -- Custom delegate implementation stays the same
    local delegate = {
        height = function(item)
            return 2
        end,

        spacing = function()
            return 1
        end,

        render = function(model, index, item)
            local is_selected = model:cursor() == index - 1
            local indicator = is_selected and "→ " or "  "
            local style = btea.new_style()

            if is_selected then
                style = style:foreground("#89B4FA"):bold()
            end

            local title_line = style:render(indicator .. item.title)
            local content_line = "   " -- Indent content line

            if item.component then
                content_line = content_line .. item.content .. " " .. item.component:view()
            else
                content_line = content_line .. item.content
            end

            return title_line .. "\n" .. content_line
        end,

        update = function(msg, model)
            local cmds = {}

            -- Update all component items
            for _, item in ipairs(items) do
                if item.component then
                    local cmd = item.component:update(msg)
                    if cmd then
                        table.insert(cmds, cmd)
                    end
                end
            end

            -- Handle key input for selected item
            if msg.key then
                local cursor = model:cursor()
                if cursor and cursor >= 0 and cursor + 1 <= #items then
                    local selected = items[cursor + 1]
                    if selected and selected.handle_key then
                        local cmd = selected:handle_key(msg, app.keys)
                        if cmd then
                            table.insert(cmds, cmd)
                        end
                    end
                end
            end

            if #cmds > 0 then
                return btea.batch(cmds)
            end
            return nil
        end
    }

    -- Create list with our mixed items
    app.list = btea.new_list({
        width = app.window.width,
        height = app.window.height - 2,
        items = items,
        delegate = delegate,
        styles = {
            title = btea.new_style():foreground("#CDD6F4"):bold(),
            status = btea.new_style():foreground("#6C7086"):italic()
        }
    })

    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.up:matches(msg) then
                self:dispatch(self.list:cursor_up())
            elseif self.keys.down:matches(msg) then
                self:dispatch(self.list:cursor_down())
            end

            local cmd = self.list:update(msg)
            if cmd then
                self:dispatch(cmd)
            end
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