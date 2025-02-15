local bapp = {}

-- Command pairs that need restoration
local RESTORE_PAIRS = {
    enter_alt_screen = "exit_alt_screen",
    hide_cursor = "show_cursor",
    enable_mouse_all_motion = "disable_mouse",
    enable_mouse_cell_motion = "disable_mouse",
    enable_bracketed_paste = "disable_bracketed_paste",
    enable_report_focus = "disable_report_focus"
}

-- Track mouse states
local MOUSE_STATES = {
    enable_mouse_all_motion = true,
    enable_mouse_cell_motion = true
}

-- Create common key bindings
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

-- Instantiate a new base app
function bapp.new(init_commands)
    local app = {
        cmd_channel = channel.new(128),
        is_running = false,
        window = { width = 80, height = 24 },
        view_ch = pubsub.subscribe("@btea/view"),
        update_ch = pubsub.subscribe("@btea/update"),
        cancel_ch = pubsub.subscribe("@cancel"),
        done = channel.new(),
        active_commands = {}, -- Track active commands that need restoration
        mouse_enabled = false, -- Track mouse state
        cursor_blink_time = 530 -- Standard cursor blink rate in ms
    }

    -- Helper to track commands that need restoration
    function app:track_command(cmd_name)
        if RESTORE_PAIRS[cmd_name] then
            self.active_commands[cmd_name] = true
            -- Update mouse state if this is a mouse command
            if MOUSE_STATES[cmd_name] then
                self.mouse_enabled = true
            end
        end
    end

    -- Generate restoration commands based on active commands
    function app:generate_restore_commands()
        local restore_cmds = {}
        -- If mouse is enabled, ensure we disable it
        if self.mouse_enabled then
            table.insert(restore_cmds, btea.commands.disable_mouse)
        end
        -- Add other restore commands
        for cmd_name, _ in pairs(self.active_commands) do
            local restore_cmd = RESTORE_PAIRS[cmd_name]
            if restore_cmd and restore_cmd ~= "disable_mouse" then -- Skip duplicate mouse disable
                table.insert(restore_cmds, btea.commands[restore_cmd])
            end
        end
        return restore_cmds
    end

    -- Dispatch a single command with tracking
    function app:dispatch(cmd)
        if not cmd then return end

        -- Track command by comparing with known btea commands
        for cmd_name, cmd_value in pairs(btea.commands) do
            if cmd == cmd_value then
                self:track_command(cmd_name)
                break
            end
        end

        self.cmd_channel:send(cmd)
    end

    -- Dispatch multiple commands
    function app:dispatch_many(cmds)
        if cmds and #cmds > 0 then
            for _, cmd in ipairs(cmds) do
                self:dispatch(cmd)
            end
        end
    end

    function app:update_window_size(size)
        self.window.width = size.width
        self.window.height = size.height
    end

    -- Initialize terminal with custom or default commands
    function app:init_terminal()
        local init_cmds = init_commands or {
            btea.commands.enter_alt_screen,
            btea.commands.hide_cursor,
            btea.commands.enable_mouse_all_motion -- Enable mouse by default
        }
        self:dispatch_many(init_cmds)

        -- Start cursor blink timer
        self:start_cursor_blink()
    end

    -- Start cursor blink timer
    function app:start_cursor_blink()
        coroutine.spawn(function()
            local ticker = time.ticker(string.format("%dms", self.cursor_blink_time))
            while true do
                local result = channel.select {
                    ticker:channel():case_receive(),
                    self.done:case_receive()
                }

                if result.channel == self.done then
                    ticker:stop()
                    break
                end

                -- Send blink update
                upstream.send({ type = "update", blink = true })
            end
        end)
    end

    -- Cleanup terminal using tracked restore commands
    function app:cleanup_terminal()
        local restore_cmds = self:generate_restore_commands()
        self:dispatch_many(restore_cmds)
        self.done:send(true)
        self.done:close()
        self.active_commands = {} -- Clear tracked commands
        self.mouse_enabled = false -- Reset mouse state
    end

    -- Command processor
    function app:start_command_processor()
        coroutine.spawn(function()
            while true do
                local result = channel.select({
                    self.cmd_channel:case_receive(),
                    self.done:case_receive()
                })
                if not result.ok then break end
                if result.channel == self.done then break end

                local cmd = result.value
                if cmd then
                    local msg = cmd:execute()
                    if msg then upstream.send(msg) end
                end
            end
        end)
    end

    -- Main run loop
    function app:run(update_fn, view_fn)
        self.is_running = true
        self:init_terminal()
        self:start_command_processor()

        while self.is_running do
            local result = channel.select({
                self.view_ch:case_receive(),
                self.update_ch:case_receive(),
                self.cancel_ch:case_receive()
            })

            if not result.ok then
                self.done:send(true)
                break
            end

            local task = result.value
            if not task then
                self.done:send(true)
                break
            end

            if result.channel == self.cancel_ch then
                break
            elseif result.channel == self.update_ch then
                local msg = task:input()
                if type(msg) == "table" then
                    if msg.window_size then
                        self:update_window_size(msg.window_size)
                    end
                    local should_quit = update_fn(self, msg)
                    if should_quit then break end
                    task:complete(view_fn(self))
                else
                    task:complete("ok")
                end
            elseif result.channel == self.view_ch then
                task:complete(view_fn(self))
            end
        end

        self:cleanup_terminal()
        pubsub.unsubscribe(self.view_ch)
        pubsub.unsubscribe(self.update_ch)
        pubsub.unsubscribe(self.cancel_ch)
    end

    -- Enable mouse with proper tracking
    function app:enable_mouse(options)
        options = options or {}
        if options.all_motion == nil or options.all_motion then
            self:dispatch(btea.commands.enable_mouse_all_motion)
        else
            self:dispatch(btea.commands.enable_mouse_cell_motion)
        end
    end

    return app
end

return bapp