local CommandHandler = {}
CommandHandler.__index = CommandHandler

function CommandHandler.new(app, client, ui)
    local self = setmetatable({}, CommandHandler)
    self.app = app
    self.client = client
    self.ui = ui
    self.commands = {}

    -- Register default commands
    self:register_command("ping", function(args)
        if self.client:get_active_channel() then
            self.client:send_message(self.client:get_active_channel().id, "Pong!")
            self.ui:add_message("<Bot> Pong!", "system")
        else
            self.ui:add_message("No active channel selected!", "system")
        end
    end, "Respond with Pong!")

    self:register_command("channels", function(args)
        local channels = self.client:get_channels(self.client.active_guild)
        if channels then
            self.ui:add_message("Available channels:", "system")
            for id, channel in pairs(channels) do
                self.ui:add_message(string.format("#%s (ID: %s)", channel.name, id), "system")
            end
        end
    end, "List available channels")

    self:register_command("listen", function(args)
        if not args[1] then
            self.ui:add_message("Usage: !listen <channel_name>", "system")
            return
        end

        local channel_name = args[1]
        local channels = self.client:get_channels(self.client.active_guild)
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
                self.ui:add_message("Channel not found: " .. channel_name, "system")
            end
        end
    end, "Listen to a specific channel")

    self:register_command("help", function(args)
        self.ui:add_message("Available commands:", "system")
        for cmd, info in pairs(self.commands) do
            self.ui:add_message(string.format("!%s - %s", cmd, info.help), "system")
        end
    end, "Show this help message")

    -- Register new !ai command for toggling AI mode
    self:register_command("ai", function(args)
        local app = self.app
        if args[1] == "on" then
            if not app.ai_mode_enabled then
                app.ai_mode_enabled = true
                app.ai_session = require("chat_components").ChatSession.new()
                self.ui:add_message("AI mode enabled.", "system")
            else
                self.ui:add_message("AI mode is already enabled.", "system")
            end
        elseif args[1] == "off" then
            if app.ai_mode_enabled then
                app.ai_mode_enabled = false
                app.ai_session = nil
                self.ui:add_message("AI mode disabled.", "system")
            else
                self.ui:add_message("AI mode is already disabled.", "system")
            end
        else
            self.ui:add_message("Usage: !ai on|off", "system")
        end
    end, "Toggle AI mode for automatic responses.")

    return self
end

function CommandHandler:register_command(name, handler, help)
    self.commands[name] = {
        handler = handler,
        help = help or "No help available"
    }
end

function CommandHandler:handle_command(input)
    if not input:match("^!") then
        return false
    end

    local command = input:match("^!(%w+)")
    local args = {}
    for arg in input:match("^!%w+%s*(.*)$"):gmatch("%S+") do
        table.insert(args, arg)
    end

    if self.commands[command] then
        self.commands[command].handler(args)
        return true
    else
        self.ui:add_message("Unknown command: " .. command, "system")
        return true
    end
end

return CommandHandler
