local bapp = require("bapp")

-- Custom model that implements tea.Model interface
local function create_click_model(zone_id, initial_text)
    return {
        zone_id = zone_id,
        text = initial_text,
        clicks = 0,

        -- Required tea.Model interface methods
        init = function(self)
            return nil
        end,

        update = function(self, msg)
            if msg.mouse and msg.mouse.action == "press" then
                self.clicks = self.clicks + 1
                self.text = string.format("Clicked %d times!", self.clicks)
            end
            return nil
        end,

        view = function(self)
            return self.text
        end
    }
end

function App()
    local app = bapp.new()
    local zone_manager = btea.new_zone_manager()

    -- Create multiple clickable models
    app.click_models = {
        create_click_model("zone1", "Click me! (0)"),
        create_click_model("zone2", "Click me too! (0)"),
        create_click_model("zone3", "And me! (0)")
    }

    -- Style for the clickable areas
    app.styles = {
        normal = btea.new_style()
            :padding(1, 2)
            :border("rounded")
            :background("#45475A"),

        container = btea.new_style()
            :border("rounded")
            :padding(1)
            :background("#1E1E2E")
    }

    -- Enable mouse support
    app.cmd_channel:send(btea.commands.enable_mouse_all_motion)

    -- Update function
    local function update(self, msg)
        if msg.key and msg.key.key_type == "ctrl+c" then
            return true
        end

        -- Handle mouse events using any_in_bounds_update
        if msg.mouse and msg.mouse.type == "mouse" then
            for _, model in ipairs(self.click_models) do
                -- any_in_bounds_update will both check if mouse is in bounds
                -- and update the model if it is
                local cmd = zone_manager:any_in_bounds_update(model, msg)
                if cmd then
                    self:dispatch(cmd)
                    break -- Stop after first hit
                end
            end
        end

        return false
    end

    -- View function
    local function view(self)
        local lines = {
            "Click the zones below:",
            ""
        }

        -- Render each clickable model with its zone
        for _, model in ipairs(self.click_models) do
            local content = self.styles.normal:render(model:view())
            table.insert(lines, zone_manager:mark(model.zone_id, content))
            table.insert(lines, "")
        end

        -- Add help text
        table.insert(lines, "Press Ctrl+C to quit")

        -- Wrap everything in container and scan for zones
        return zone_manager:scan(
            self.styles.container:render(
                table.concat(lines, "\n")
            )
        )
    end

    -- Run the app
    app:run(update, view)
end

return App
