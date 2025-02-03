local bapp = {}

-- Default styles that can be used across apps
bapp.styles = {
    base = btea.new_style()
        :border(btea.borders.ROUNDED)
        :padding(1)
        :background("#1E1E2E"),

    title = btea.new_style()
        :foreground("#CDD6F4")
        :bold(),

    help = btea.new_style()
        :foreground("#6C7086")
        :italic()
}

-- Helper to create common key bindings
function bapp.create_keys(bindings)
    local keys = {}
    -- Always include quit binding
    keys.quit = btea.new_binding({
        keys = bindings.quit or { "q", "ctrl+c" },
        help = { key = "q/^C", desc = "quit" }
    })

    -- Add additional bindings
    for name, binding in pairs(bindings) do
        if name ~= "quit" then
            keys[name] = btea.new_binding(binding)
        end
    end

    return keys
end

-- Create a new base app with channels setup
function bapp.new()
    local app = {
        inbox = tasks.channel(),
        done = channel.new(),
        cmd_channel = channel.new(128),
        is_running = false
    }

    -- Initialize terminal
    function app:init_terminal()
        self.cmd_channel:send(btea.batch({
            btea.commands.enter_alt_screen,
            btea.commands.hide_cursor
        }))
    end

    -- Cleanup terminal
    function app:cleanup_terminal()
        self.cmd_channel:send(btea.batch({
            btea.commands.show_cursor,
            btea.commands.exit_alt_screen
        }))
        self.done:close()
    end

    -- Start command processor coroutine
    function app:start_command_processor()
        coroutine.spawn(function()
            while true do
                local result = channel.select({
                    self.cmd_channel:case_receive(),
                    self.done:case_receive()
                })

                if result.channel == self.done then
                    break
                else
                    local cmd = result.value
                    if cmd then
                        local msg = cmd:execute()
                        if msg then upstream.send(msg) end
                    end
                end
            end
        end)
    end

    -- Run the application with given update and view functions
    function app:run(update_fn, view_fn)
        self.is_running = true
        self:init_terminal()
        self:start_command_processor()

        while self.is_running do
            local task, ok = self.inbox:receive()
            if not ok then
                self.done:send(true)
                break
            end

            local msg = task:input()
            if type(msg) == "table" then
                if msg.type == "update" then
                    local should_quit = update_fn(self, msg)
                    if should_quit then break end
                    task:complete(view_fn(self))
                elseif msg.type == "view" then
                    task:complete(view_fn(self))
                else
                    task:complete("ok")
                end
            else
                task:complete("ok")
            end
        end

        self:cleanup_terminal()
    end

    return app
end

return bapp
