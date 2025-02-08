local bapp = require("bapp")

function dump(o)
    if type(o) == 'table' then
        local s = '{ '
        for k, v in pairs(o) do
            if type(k) ~= 'number' then k = '"' .. k .. '"' end
            s = s .. '[' .. k .. '] = ' .. dump(v) .. ','
        end
        return s .. '} '
    else
        return tostring(o)
    end
end

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
            --print(dump(msg))

            -- Handle zone_in_bounds messages
            if msg.zone_in_bounds and msg.zone_in_bounds.type == "zone_in_bounds" then
                -- Check if the mouse event is a press
                if msg.zone_in_bounds.event.action == "press" then
                    self.clicks = self.clicks + 1
                    self.text = string.format("Clicked %d times!", self.clicks)
                    return btea.commands.refresh
                end
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

    app.clicks = 0

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
                self:dispatch(zone_manager:any_in_bounds_update(model, msg))
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
