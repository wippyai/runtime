local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Initialize state
    app.state = {
        x = 10,
        y = 5,
        width = 40,
        height = 20,
    }

    -- Setup key bindings with help text
    app.keys = bapp.create_keys({
        up = {
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        },
        down = {
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        },
        left = {
            keys = {"left", "h"},
            help = {key = "←/h", desc = "move left"}
        },
        right = {
            keys = {"right", "l"},
            help = {key = "→/l", desc = "move right"}
        }
    })

    -- Define styles
    app.styles = {
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        player = btea.style()
            :foreground("#F5C2E7")
            :bold(),

        help = btea.style()
            :foreground("#6C7086")
            :italic()
    }

    -- Create help text
    app.help_text = "Move with arrows/hjkl | q/^C to quit"

    -- Update function
    local function update(self, msg)
        -- Handle window size updates
        if msg.window_size then
            self.state.width = math.min(msg.window_size.width - 4, 40)
            self.state.height = math.min(msg.window_size.height - 4, 20)
            -- Keep player in bounds after resize
            self.state.x = math.min(self.state.x, self.state.width)
            self.state.y = math.min(self.state.y, self.state.height)
        end

        -- Handle key presses
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.up:matches(msg) then
                self.state.y = math.max(1, self.state.y - 1)
            elseif self.keys.down:matches(msg) then
                self.state.y = math.min(self.state.height, self.state.y + 1)
            elseif self.keys.left:matches(msg) then
                self.state.x = math.max(1, self.state.x - 1)
            elseif self.keys.right:matches(msg) then
                self.state.x = math.min(self.state.width, self.state.x + 1)
            end
        end

        return false
    end

    -- View function
    local function view(self)
        local lines = {}
        
        -- Draw the game area
        for y = 1, self.state.height do
            local line = ""
            for x = 1, self.state.width do
                if x == self.state.x and y == self.state.y then
                    line = line .. self.styles.player:render("@")
                else
                    line = line .. " "
                end
            end
            table.insert(lines, line)
        end

        -- Add help text at the bottom
        table.insert(lines, "")
        table.insert(lines, self.styles.help:render(self.help_text))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App