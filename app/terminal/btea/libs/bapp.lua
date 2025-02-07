local bapp = {}

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

-- Helper to create tabbed layout
function bapp.create_tabs(tabs_config)
    local tabs = {
        titles = tabs_config.titles or {},
        content = tabs_config.content or {},
        active = 1,
        window_size = { width = 80, height = 24 },
        keys = {
            next = btea.new_binding({
                keys = { "right", "l", "n", "tab" },
                help = { key = "→/tab", desc = "next tab" }
            }),
            prev = btea.new_binding({
                keys = { "left", "h", "p", "shift+tab" },
                help = { key = "←/shift+tab", desc = "prev tab" }
            })
        }
    }

    -- Add tab navigation methods
    function tabs:next()
        self.active = math.min(self.active + 1, #self.titles)
    end

    function tabs:prev()
        self.active = math.max(self.active - 1, 1)
    end

    function tabs:handle_key(msg)
        if self.keys.next:matches(msg) then
            self:next()
            return true
        elseif self.keys.prev:matches(msg) then
            self:prev()
            return true
        end
        return false
    end

    function tabs:update_window_size(size)
        self.window_size = size
    end

    return tabs
end

-- Create a new base app with channels setup
function bapp.new()
    local app = {
        inbox = tasks.channel(),
        done = channel.new(),
        cmd_channel = channel.new(128),
        is_running = false,
        window = {
            width = 80, -- Default width
            height = 24 -- Default height
        }
    }

    -- Dispatch command helper - handles nil commands gracefully
    function app:dispatch(cmd)
        if cmd then
            self.cmd_channel:send(cmd)
        end
    end

    -- Dispatch multiple commands
    function app:dispatch_many(cmds)
        if cmds and #cmds > 0 then
            self.cmd_channel:send(btea.batch(cmds))
        end
    end

    -- Update window size and notify components
    function app:update_window_size(size)
        self.window.width = size.width
        self.window.height = size.height

        -- Update any components that need window size
        if self.tabs then
            self.tabs:update_window_size(size)
        end

        -- Viewport if exists
        if self.viewport then
            self.viewport:set_width(size.width)
            self.viewport:set_height(size.height)
        end
    end

    -- Initialize terminal
    function app:init_terminal()
        self:dispatch_many({
            btea.commands.enter_alt_screen,
            btea.commands.hide_cursor
        })
    end

    -- Cleanup terminal
    function app:cleanup_terminal()
        self:dispatch_many({
            btea.commands.show_cursor,
            btea.commands.exit_alt_screen
        })
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
                    -- Handle window size updates
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
    end

    -- Mouse support helper
    function app:enable_mouse(options)
        options = options or {}
        if options.all_motion or options.all_motion == nil then
            self:dispatch(btea.commands.enable_mouse_all_motion)
        end
    end

    return app
end

return bapp
