local bapp = require("bapp")
local llm = require("llm")
local components = require("components")
local time = require("time")

function App()
    -- Create app with proper init commands
    local init_commands = {
       -- btea.commands.enter_alt_screen,
      --  btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Initialize state
    app.messages = {}
    app.llm = llm.LLMClient.new()
    app.session = components.ChatSession.new()
    app.update_channel = channel.new()

    -- Initialize text input with proper width
    app.input = btea.text_input({
        prompt = "> ",
        placeholder = "Type something...",
        width = app.window.width - 8
    })

    -- Focus the input immediately
    app:dispatch(app.input:focus())

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "esc"},
            help = {key = "^C/esc", desc = "quit"}
        }),
        submit = btea.bind({
            keys = {"enter"},
            help = {key = "enter", desc = "send message"}
        })
    }

    -- Start LLM update listener
    coroutine.spawn(function()
        local ai_response = nil
        while true do
            local msg, ok = app.update_channel:receive()
            if not ok then break end

            if msg.type == "update" then
                app.session:update_response(msg.text)
                if not ai_response then
                    ai_response = msg.text
                    table.insert(app.messages, {
                        type = "ai",
                        content = ai_response,
                        timestamp = time.now()
                    })
                else
                    ai_response = ai_response .. msg.text
                    app.messages[#app.messages].content = ai_response
                end
                app:upstream("refresh")
            elseif msg.type == "done" then
                app.session:finish_response()
                ai_response = nil
            elseif msg.type == "error" then
                table.insert(app.messages, {
                    type = "error",
                    content = tostring(msg.error),
                    timestamp = time.now()
                })
                app:upstream("refresh")
            end
        end
    end)

    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.input:set_width(self.window.width - 8)
        end

        -- Update text input
        local cmd = self.input:update(msg)
        if cmd then
            self:dispatch(cmd)
        end

        -- Handle key events
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.submit:matches(msg) then
                local value = self.input:value()
                if value ~= "" then
                    -- Add user message
                    table.insert(self.messages, {
                        type = "user",
                        content = value,
                        timestamp = time.now()
                    })

                    -- Add to session and start response
                    self.session:add_message("user", value)
                    self.session:start_response()

                    -- Query LLM
                    self.llm:query(value, self.session:get_history(), self.update_channel)

                    self.input:set_value("")
                end
            end
        end

        return false
    end

    -- View rendering
    local function view(self)
        local content_width = self.window.width - 6
        local header_divider = string.rep("─", content_width)

        local content = {
            components.styles.header:render("Chat"),
            components.styles.timestamp:render(header_divider)
        }

        -- Calculate visible messages area
        local max_visible = self.window.height - 8
        local start_idx = math.max(1, #self.messages - max_visible)

        -- Add messages with timestamps
        for i = start_idx, #self.messages do
            local msg = self.messages[i]
            local timestamp = msg.timestamp:format("15:04:05")
            local styled_time = components.styles.timestamp:render(timestamp)
            local style = components.styles[msg.type]
            local styled_text = style:render(msg.content)
            table.insert(content, styled_time .. " " .. styled_text)
        end

        -- Add input field
        table.insert(content, "")
        table.insert(content, self.input:view())

        return components.styles.box
            :width(self.window.width - 2)
            :height(self.window.height - 2)
            :render(table.concat(content, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App