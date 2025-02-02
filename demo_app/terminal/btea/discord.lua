local function App()
    -- App channels and state
    local inbox = tasks.channel()
    local done = channel.new()
    local messages = {}
    local window_width = 80
    local window_height = 24

    -- Initialize text input
    local input = btea.new_textinput()
    input:placeholder("Type a message or command (!help, !channels, !listen)...")
    input:set_width(window_width - 8)
    local current_cmd = input:focus() -- Get initial focus
    if current_cmd then
        upstream.send(current_cmd)
    end

    -- Discord state
    local discord = require("discord_client")
    local connection_status = "Starting..."
    local active_guild = nil
    local active_channel = nil

    -- Styles
    local styles = {
        box = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.new_style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1),

        status = btea.new_style()
            :foreground("#F9E2AF")
            :bold(),

        message = btea.new_style()
            :foreground("#89B4FA"),

        system = btea.new_style()
            :foreground("#F38BA8")
            :bold(),

        timestamp = btea.new_style()
            :foreground("#6C7086"),

        input_area = btea.new_style()
            :background("#313244")
            :padding(0, 1)
            :margin(1, 0)
    }

    local function format_message(text, type)
        local timestamp = styles.timestamp:render(time.now():format("15:04:05"))
        if type == "system" then
            return timestamp .. " " .. styles.system:render(text)
        else
            return timestamp .. " " .. styles.message:render(text)
        end
    end

    local function create_view(operations, input_view)
        local content_width = window_width - 6
        local header_divider = string.rep("─", content_width)

        local display_content = {
            styles.header:render("Discord Bot"),
            styles.timestamp:render(header_divider),
            styles.status:render("Status: " .. connection_status),
            styles.status:render("Channel: " .. (active_channel and "#" .. active_channel.name or "None")),
            styles.timestamp:render(header_divider)
        }

        -- Calculate visible messages area
        local max_visible = window_height - 10
        local start_idx = math.max(1, #operations - max_visible)

        for i = start_idx, #operations do
            table.insert(display_content, operations[i])
        end

        -- Add input field
        table.insert(display_content, "")
        table.insert(display_content, styles.input_area:render(input_view))

        return styles.box
            :width(window_width - 2)
            :height(window_height - 2)
            :render(table.concat(display_content, "\n"))
    end

    local client = nil

    -- Create Discord client
    client = discord.Client.new(env.get("DISCORD_BOT_TOKEN"))

    client
        :on("connect", function()
            table.insert(messages, format_message("Connected to Discord gateway", "system"))
            connection_status = "Connected"
        end)
        :on("disconnect", function()
            table.insert(messages, format_message("Disconnected from Discord", "system"))
            connection_status = "Disconnected"
        end)
        :on("error", function(err)
            table.insert(messages, format_message("Error: " .. tostring(err), "system"))
            connection_status = "Error"
        end)
        :on("ready", function(data)
            table.insert(messages, format_message("Bot is ready! Logged in as " .. data.user.username, "system"))
            connection_status = "Ready"

            table.insert(messages, format_message("Log info " .. json.encode(data), "system"))

            if data.guilds and #data.guilds > 0 then
                active_guild = data.guilds[1].id
                local channels = client:get_channels(active_guild)
                if channels then
                    table.insert(messages, format_message("Available channels:", "system"))
                    for id, channel in pairs(channels) do
                        table.insert(messages, format_message(string.format("#%s (ID: %s)", channel.name, id), "system"))
                    end
                end
            end
        end)
        :on("channel_change", function(channel)
            table.insert(messages, format_message("Now listening to #" .. channel.name, "system"))
            active_channel = channel
        end)
        :on("message", function(msg)
            if not msg.author.bot then
                table.insert(messages,
                    format_message(string.format("<%s> %s", msg.author.username, msg.content), "message"))

                if msg.content:match("^!") then
                    if msg.content:match("^!ping") then
                        client:send_message(msg.channel_id, "Pong!")
                    elseif msg.content:match("^!listen%s+(.+)") then
                        local channel_name = msg.content:match("^!listen%s+(.+)")
                        local channels = client:get_channels(active_guild)
                        if channels then
                            for id, channel in pairs(channels) do
                                if channel.name == channel_name then
                                    client:set_active_channel(id)
                                    break
                                end
                            end
                        end
                    elseif msg.content:match("^!channels") then
                        local channels = client:get_channels(active_guild)
                        if channels then
                            table.insert(messages, format_message("Available channels:", "system"))
                            for id, channel in pairs(channels) do
                                table.insert(messages,
                                    format_message(string.format("#%s (ID: %s)", channel.name, id), "system"))
                            end
                        end
                    end
                end
            end
        end)

    -- Start Discord client
    client:start()

    -- Start ticker for UI updates
    coroutine.spawn(function()
        local ticker = time.ticker("1s")
        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }
            if result.channel == done then break end
            upstream.send("tick")
        end
    end)

    -- Main UI loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if type(msg) == "table" and msg.type == "update" then
            -- Update window size if available
            if msg.window_size then
                window_width = msg.window_size.width
                window_height = msg.window_size.height
                input:set_width(window_width - 8)
            end

            -- Check for enter key BEFORE updating input
            if msg.key and msg.key.key_type == "enter" then
                local value = input:value()
                if value ~= "" then
                    -- Handle the message
                    if value:match("^!") then
                        -- Handle commands
                        if value:match("^!channels") then
                            local channels = client:get_channels(active_guild)
                            if channels then
                                table.insert(messages, format_message("Available channels:", "system"))
                                for id, channel in pairs(channels) do
                                    table.insert(messages,
                                        format_message(string.format("#%s (ID: %s)", channel.name, id), "system"))
                                end
                            end
                        elseif value:match("^!listen%s+(.+)") then
                            local channel_name = value:match("^!listen%s+(.+)")
                            local channels = client:get_channels(active_guild)
                            if channels then
                                local found = false
                                for id, channel in pairs(channels) do
                                    if channel.name == channel_name then
                                        client:set_active_channel(id)
                                        found = true
                                        break
                                    end
                                end
                                if not found then
                                    table.insert(messages,
                                        format_message("Channel not found: " .. channel_name, "system"))
                                end
                            end
                        elseif value:match("^!ping") then
                            if active_channel then
                                client:send_message(active_channel.id, "Pong!")
                                table.insert(messages, format_message("<Bot> Pong!", "system"))
                            else
                                table.insert(messages, format_message("No active channel selected!", "system"))
                            end
                        end
                    else
                        -- Send regular message
                        if active_channel then
                            client:send_message(active_channel.id, value)
                            table.insert(messages, format_message(string.format("<You> %s", value), "message"))
                        else
                            table.insert(messages,
                                format_message("No active channel. Use !listen <channel> first", "system"))
                        end
                    end
                    -- Clear input after handling
                    input:set_value("")
                end
            end

            -- Update input and handle any commands
            local cmd = input:update(msg)
            if cmd then
                task:complete(cmd)
            else
                task:complete("ok")
            end
        elseif type(msg) == "table" and msg.type == "view" then
            task:complete(create_view(messages, input:view()))
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App
