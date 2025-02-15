local bapp = require("bapp")
local discord_client = require("discord_client")
local time = require("time")
local json = require("json")
local env = require("env")
local channel = require("channel")
local tasks = require("tasks")

function App()
    -- Create app with proper init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- App channels and state
    app.messages = {}
    app.connection_status = "Starting..."
    app.active_guild = nil
    app.active_channel = nil

    -- Create zone manager for mouse interactions
    app.zone_manager = btea.zone_manager()

    -- Initialize text input with proper styling
    app.input = btea.text_input({
        prompt = "> ",
        placeholder = "Type a message or command (!help, !channels, !listen)...",
        width = app.window.width - 8
    })

    -- Define styles
    app.styles = {
        box = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1),

        status = btea.style()
            :foreground("#F9E2AF")
            :bold(),

        message = btea.style()
            :foreground("#89B4FA"),

        system = btea.style()
            :foreground("#F38BA8")
            :bold(),

        timestamp = btea.style()
            :foreground("#6C7086"),

        input_area = btea.style()
            :background("#313244")
            :padding(0, 1)
            :margin(1, 0)
    }

    -- Define key bindings using bapp.create_keys
    app.keys = bapp.create_keys({
        quit = {
            keys = { "ctrl+c", "q", "esc" },
            help = { key = "^C/q/esc", desc = "quit" }
        },
        submit = {
            keys = { "enter" },
            help = { key = "enter", desc = "send message" }
        }
    })

    -- Helper functions
    local function format_message(text, type)
        local timestamp = app.styles.timestamp:render(time.now():format("15:04:05"))
        if type == "system" then
            return timestamp .. " " .. app.styles.system:render(text)
        else
            return timestamp .. " " .. app.styles.message:render(text)
        end
    end

    -- Initialize Discord client
    app.client = discord_client.Client.new(env.get("DISCORD_BOT_TOKEN"))

    -- Setup client event handlers
    app.client
        :on("connect", function()
            table.insert(app.messages, format_message("Connected to Discord gateway", "system"))
            app.connection_status = "Connected"
        end)
        :on("disconnect", function()
            table.insert(app.messages, format_message("Disconnected from Discord", "system"))
            app.connection_status = "Disconnected"
        end)
        :on("error", function(err)
            table.insert(app.messages, format_message("Error: " .. tostring(err), "system"))
            app.connection_status = "Error"
        end)
        :on("ready", function(data)
            table.insert(app.messages, format_message("Bot is ready! Logged in as " .. data.user.username, "system"))
            app.connection_status = "Ready"

            if data.guilds and #data.guilds > 0 then
                app.active_guild = data.guilds[1].id
                local channels = app.client:get_channels(app.active_guild)
                if channels then
                    table.insert(app.messages, format_message("Available channels:", "system"))
                    for id, channel in pairs(channels) do
                        table.insert(app.messages, format_message(string.format("#%s (ID: %s)", channel.name, id), "system"))
                    end
                end
            end
        end)
        :on("channel_change", function(channel)
            table.insert(app.messages, format_message("Now listening to #" .. channel.name, "system"))
            app.active_channel = channel
        end)
        :on("message", function(msg)
            if not msg.author.bot then
                table.insert(app.messages,
                    format_message(string.format("<%s> %s", msg.author.username, msg.content), "message"))

                if msg.content:match("^!") then
                    -- Handle commands
                    if msg.content:match("^!ping") then
                        app.client:send_message(msg.channel_id, "Pong!")
                    elseif msg.content:match("^!listen%s+(.+)") then
                        local channel_name = msg.content:match("^!listen%s+(.+)")
                        local channels = app.client:get_channels(app.active_guild)
                        if channels then
                            for id, channel in pairs(channels) do
                                if channel.name == channel_name then
                                    app.client:set_active_channel(id)
                                    break
                                end
                            end
                        end
                    elseif msg.content:match("^!channels") then
                        local channels = app.client:get_channels(app.active_guild)
                        if channels then
                            table.insert(app.messages, format_message("Available channels:", "system"))
                            for id, channel in pairs(channels) do
                                table.insert(app.messages,
                                    format_message(string.format("#%s (ID: %s)", channel.name, id), "system"))
                            end
                        end
                    end
                end
            end
        end)

    -- Start Discord client
    app.client:start()

    -- Update handler
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.input:set_width(self.window.width - 8)
        end

        -- Handle key events
        if type(msg) == "table" and msg.type == "update" then
            if msg.key then
                if self.keys.quit:matches(msg) then
                    return true -- signal quit
                elseif self.keys.submit:matches(msg) then
                    local value = self.input:value()
                    if value ~= "" then
                        -- Handle commands
                        if value:match("^!") then
                            if value:match("^!channels") then
                                local channels = self.client:get_channels(self.active_guild)
                                if channels then
                                    table.insert(self.messages, format_message("Available channels:", "system"))
                                    for id, channel in pairs(channels) do
                                        table.insert(self.messages,
                                            format_message(string.format("#%s (ID: %s)", channel.name, id), "system"))
                                    end
                                end
                            elseif value:match("^!listen%s+(.+)") then
                                local channel_name = value:match("^!listen%s+(.+)")
                                local channels = self.client:get_channels(self.active_guild)
                                if channels then
                                    local found = false
                                    for id, channel in pairs(channels) do
                                        if channel.name == channel_name then
                                            self.client:set_active_channel(id)
                                            found = true
                                            break
                                        end
                                    end
                                    if not found then
                                        table.insert(self.messages,
                                            format_message("Channel not found: " .. channel_name, "system"))
                                    end
                                end
                            elseif value:match("^!ping") then
                                if self.active_channel then
                                    self.client:send_message(self.active_channel.id, "Pong!")
                                    table.insert(self.messages, format_message("<Bot> Pong!", "system"))
                                else
                                    table.insert(self.messages, format_message("No active channel selected!", "system"))
                                end
                            end
                        else
                            -- Send regular message
                            if self.active_channel then
                                self.client:send_message(self.active_channel.id, value)
                                table.insert(self.messages, format_message(string.format("<You> %s", value), "message"))
                            else
                                table.insert(self.messages,
                                    format_message("No active channel. Use !listen <channel> first", "system"))
                            end
                        end
                        -- Clear input after handling
                        self.input:set_value("")
                    end
                end
            end
        end

        -- Update input and handle any commands
        local cmd = self.input:update(msg)
        if cmd then self:dispatch(cmd) end

        return false -- continue running
    end

    -- View rendering
    local function view(self)
        local content_width = self.window.width - 6
        local header_divider = string.rep("─", content_width)

        local content = {
            self.styles.header:render("Discord Bot"),
            self.styles.timestamp:render(header_divider),
            self.styles.status:render("Status: " .. self.connection_status),
            self.styles.status:render("Channel: " .. (self.active_channel and "#" .. self.active_channel.name or "None")),
            self.styles.timestamp:render(header_divider)
        }

        -- Calculate visible messages area
        local max_visible = self.window.height - 10
        local start_idx = math.max(1, #self.messages - max_visible)

        for i = start_idx, #self.messages do
            table.insert(content, self.messages[i])
        end

        -- Add input field
        table.insert(content, "")
        table.insert(content, self.styles.input_area:render(self.input:view()))

        return self.styles.box
            :width(self.window.width - 2)
            :height(self.window.height - 2)
            :render(table.concat(content, "\n"))
    end

    -- Focus the input immediately
    app:dispatch(app.input:focus())

    -- Run the app
    app:run(update, view)
end

return App