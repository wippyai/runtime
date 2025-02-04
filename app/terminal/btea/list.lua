local bapp = require("bapp")

function App()
    local app = bapp.new()

    local items = {
        {
            title = "Buy groceries",
            description = "Milk, eggs, bread, cheese"
        },
        {
            title = "Finish report",
            description = "Due by end of day Friday"
        }
    }

    local delegate = {
        height = function() return 1 end,
        spacing = function() return 1 end,
        render = function(m, index, item)
            return item.title .. " - " .. item.description
        end
    }

    app.list = btea.new_list({
        width = app.window.width,
        height = app.window.height - 2,
        items = items,
        delegate = delegate
    })

    -- Setup key bindings
    app.keys = bapp.create_keys({
        quit = { keys = { "q", "ctrl+c" } },
        up = { keys = { "up", "k" } },
        down = { keys = { "down", "j" } }
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
