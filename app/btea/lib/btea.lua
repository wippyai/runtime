local bapp = {}

-- Create common key bindings.
function bapp.create_keys(bindings)
    local keys = {}
    keys.quit = btea.bind({
        keys = bindings.quit or { "q", "ctrl+c" },
        help = { key = "q/^C", desc = "quit" }
    })
    for name, binding in pairs(bindings) do
        if name ~= "quit" then
            keys[name] = btea.bind(binding)
        end
    end
    return keys
end

-- Instantiate a new base app.
function bapp.new()
    local app = {
        inbox = tasks.channel(),
        done = channel.new(),
        cmd_channel = channel.new(128),
        is_running = false,
        window = { width = 80, height = 24 },
        view_ch = pubsub.subscribe("@btea/view"),
        update_ch = pubsub.subscribe("@btea/update"),
        cancel_ch = pubsub.subscribe("@cancel")
    }

    -- Dispatch a single command.
    function app:dispatch(cmd)
        if cmd then self.cmd_channel:send(cmd) end
    end

    -- Dispatch multiple commands.
    function app:dispatch_many(cmds)
        if cmds and #cmds > 0 then
            self.cmd_channel:send(btea.batch(cmds))
        end
    end

    function app:update_window_size(size)
        self.window.width = size.width
        self.window.height = size.height
    end

    function app:init_terminal()
        self:dispatch_many({
            btea.commands.enter_alt_screen,
            btea.commands.hide_cursor
        })
    end

    function app:cleanup_terminal()
        self:dispatch_many({
            btea.commands.show_cursor,
            btea.commands.exit_alt_screen
        })
        self.done:send(true)
        self.done:close()
    end

    -- Command processor: listens on cmd_channel and executes commands.
    function app:start_command_processor()
        coroutine.spawn(function()
            while true do
                local result = channel.select({
                    self.cmd_channel:case_receive(),
                    self.done:case_receive()
                })
                if result.channel == self.done then break end
                local cmd = result.value
                if cmd then
                    local msg = cmd:execute()
                    if msg then upstream.send(msg) end
                end
            end
        end)
    end

    -- Subscribe to the dedicated channels and funnel tasks into the unified inbox.
    function app:start_subscriptions()
        coroutine.spawn(function()
            while true do
                local result = channel.select({
                    self.view_ch:case_receive(),
                    self.update_ch:case_receive(),
                    self.cancel_ch:case_receive()
                })
                if not result.ok then break end
                local task = result.value
                self.inbox:send(task)
            end
        end)
    end

    -- Main run loop: processes tasks from the unified inbox.
    function app:run(update_fn, view_fn)
        self.is_running = true
        self:init_terminal()
        self:start_command_processor()
        self:start_subscriptions()

        while self.is_running do
            local task, ok = self.inbox:receive()
            if not ok then
                self.done:send(true)
                break
            end

            local msg = task:input()
            if type(msg) == "table" then
                if msg.type == "cancel" then
                    break
                elseif msg.type == "update" then
                    if msg.window_size then
                        self:update_window_size(msg.window_size)
                    end
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
        pubsub.unsubscribe(self.view_ch)
        pubsub.unsubscribe(self.update_ch)
        pubsub.unsubscribe(self.cancel_ch)
    end

    function app:enable_mouse(options)
        options = options or {}
        if options.all_motion == nil or options.all_motion then
            self:dispatch(btea.commands.enable_mouse_all_motion)
        end
    end

    return app
end

return bapp
