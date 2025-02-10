function App()
    local inbox = tasks.channel()
    local done = channel.new()
    local data_points = {}
    local max_points = 40  -- Width of our chart
    local chart_height = 15  -- Height of our chart

    -- Generate a sine wave with some noise
    local function generate_point()
        local time = time.now()
        local seconds = time:unix()
        local base = math.sin(seconds * 0.2) * 0.5 + 0.5  -- Sine wave between 0 and 1
        local noise = math.random(-10, 10) / 100  -- Random noise
        return math.max(0, math.min(1, base + noise))  -- Clamp between 0 and 1
    end

    -- Start data generator in background
    coroutine.spawn(function()
        local ticker = time.ticker("0.2s")  -- Update 5 times per second for smooth animation
        while true do
            local result = channel.select{
                ticker:channel():case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                break
            end

            -- Add new data point
            table.insert(data_points, generate_point())
            -- Keep only last max_points
            while #data_points > max_points do
                table.remove(data_points, 1)
            end

            upstream.send("tick")
        end
    end)

    -- Helper function to get character for a specific height
    local function get_chart_char(value, row_height, total_height)
        local val_height = value * total_height
        local char_position = row_height - val_height

        if char_position <= -0.5 then
            return "█"  -- Full block
        elseif char_position <= 0 then
            return "▀"  -- Upper half block
        elseif char_position <= 0.5 then
            return "▄"  -- Lower half block
        else
            return " "  -- Empty space
        end
    end

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if msg.type == "update" then
            task:complete("ok")
        elseif msg.type == "view" then
            local view = "\n  Real-time Signal Visualization\n\n"

            -- Add top border
            view = view .. "  ┌" .. string.rep("─", max_points + 2) .. "┐\n"

            -- Generate chart rows
            for row = chart_height, 1, -1 do
                view = view .. "  │ "
                for i, value in ipairs(data_points) do
                    view = view .. get_chart_char(value, row, chart_height)
                end
                -- Fill empty space if not enough data points
                view = view .. string.rep(" ", max_points - #data_points) .. " │"

                -- Add scale on the right
                if row == chart_height then
                    view = view .. " 1.0"
                elseif row == 1 then
                    view = view .. " 0.0"
                elseif row == math.floor(chart_height/2) then
                    view = view .. " 0.5"
                end

                view = view .. "\n"
            end

            -- Add bottom border
            view = view .. "  └" .. string.rep("─", max_points + 2) .. "┘\n"

            -- Add time scale
            view = view .. "    " .. string.rep("─", max_points) .. "\n"
            view = view .. "    now " .. string.rep(" ", max_points-8) .. "-8 sec\n\n"

            -- Add legend
            view = view .. "  Press 'q' to quit\n"

            task:complete(view)
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App