local bapp = require("bapp")
local discord_client = require("discord_client")
local env = require("env")
local DiscordUI = require("discord_ui")
local CommandHandler = require("discord_commands")

function App()
    -- Create app with proper init commands
    local app = bapp.new({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    })

    -- Initialize UI
    app.ui = DiscordUI.new(app.window)

    -- Initialize Discord client
    app.client = discord_client.Client.new(env.get("DISCORD_BOT_TOKEN"))

    -- Additional initialization for AI mode:
    app.ai_mode_enabled = false
    app.ai_session = nil
    app.llm_client = require("llm").LLMClient.new()

    -- Initialize command handler (passing app for AI toggling)
    app.command_handler = CommandHandler.new(app, app.client, app.ui)

    -- Define key bindings
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

    -- Setup client event handlers
    app.client
        :on("connect", function()
            app.ui:add_message("Connected to Discord gateway", "system")
            app.ui:set_status("Connected")
        end)
        :on("disconnect", function()
            app.ui:add_message("Disconnected from Discord", "system")
            app.ui:set_status("Disconnected")
        end)
        :on("error", function(err)
            app.ui:add_message("Error: " .. tostring(err), "system")
            app.ui:set_status("Error")
        end)
        :on("ready", function(data)
            app.ui:add_message("Bot is ready! Logged in as " .. data.user.username, "system")
            app.ui:set_status("Ready")
            if data.guilds and #data.guilds > 0 then
                app.client.active_guild = data.guilds[1].id
                app.command_handler:handle_command("!channels")
            end
        end)
        :on("channel_change", function(channel)
            app.ui:add_message("Now listening to #" .. channel.name, "system")
            app.ui:set_active_channel(channel)
        end)
        :on("message", function(msg)
            -- If message has attachments, display them nicely
            if msg.attachments and #msg.attachments > 0 then
                for _, attachment in ipairs(msg.attachments) do
                    local attachment_info = string.format(
                        "📎 File: %s (%.2f KB)\n   URL: %s",
                        attachment.filename,
                        attachment.size / 1024,
                        attachment.url
                    )
                    app.ui:add_message(attachment_info, "system")
                end
            end

            if not msg.author.bot then
                app.ui:add_message(string.format("<%s> %s", msg.author.username, msg.content), "message")
                if msg.content:match("^!") then
                    app.command_handler:handle_command(msg.content)
                else
                    if app.ai_mode_enabled then
                        local result, err = app.llm_client:query_direct(msg.content, app.ai_session:get_history())
                        if err then
                            app.client:send_message(msg.channel_id, "Error: " .. tostring(err))
                        else
                            app.ai_session:add_message("assistant", result)
                            app.client:send_message(msg.channel_id, result)
                        end
                    else
                        -- Optionally handle non-command messages when AI mode is off
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
            self.ui:set_window_size(msg.window_size)
        end

        -- Handle key events
        if type(msg) == "table" and msg.type == "update" then
            if msg.key then
                if self.keys.quit:matches(msg) then
                    return true -- signal quit
                elseif self.keys.submit:matches(msg) then
                    local value = self.ui:get_input_value()
                    if value ~= "" then
                        if value:match("^!") then
                            self.command_handler:handle_command(value)
                        else
                            if self.client:get_active_channel() then
                                self.client:send_message(self.client:get_active_channel().id, value)
                                self.ui:add_message(string.format("<You> %s", value), "message")
                            else
                                self.ui:add_message("No active channel. Use !listen <channel> first", "system")
                            end
                        end
                        self.ui:clear_input()
                    end
                end
            end
        end

        -- Update input and handle any commands
        local cmd = self.ui:update(msg)
        if cmd then self:dispatch(cmd) end

        return false -- continue running
    end

    -- View rendering
    local function view(self)
        return self.ui:render()
    end

    -- Focus the input immediately
    app:dispatch(app.ui:focus())

    -- Run the app
    app:run(update, view)
end

return App
