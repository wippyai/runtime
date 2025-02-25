local bapp = require("bapp")
local env = require("env")
local time = require("time")

-- Import our libraries
local UI = require("ui_lib")
local ClaudeClient = require("claude_client")
local AgentHandler = require("agent_handler")
local Session = require("session_lib")

-- Simple Claude TUI Application
function App()
    -- Create app with proper init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }
    local app = bapp.new(init_commands)

    -- Initialize state
    app.messages = {}
    app.debug_logs = {}
    app.show_debug = true
    app.is_processing = false
    app.current_tool_use = nil

    -- Initialize UI library
    app.ui = UI.new()
    app = app.ui:init_components(app)

    -- Initialize API client
    app.client = ClaudeClient.new(env.get("ANTHROPIC_API_KEY"))

    -- Initialize Agent handler
    app.agent_handler = AgentHandler.new(app)

    -- Initialize Session manager
    app.session = Session.new(app, app.client, app.agent_handler)

    -- Add system message to show connection
    app.ui:add_system_message(app, "Connected to Claude")

    -- Shortcut methods for adding messages
    app.add_system_message = function(self, message)
        self.ui:add_system_message(self, message)
    end

    app.log_debug = function(self, message)
        self.ui:log_debug(self, message)
    end

    -- Update function
    local function update(self, msg)
        return self.ui:update(self, msg)
    end

    -- View function
    local function view(self)
        return self.ui:render(self)
    end

    -- Run the app
    app:run(update, view)
end

return App
