function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Create key bindings
    local keys = {
        quit = btea.new_binding({
            keys = {"q", "ctrl+c"},
            help = {key = "q/^C", desc = "quit"}
        }),
        toggle_help = btea.new_binding({
            keys = {"?"},
            help = {key = "?", desc = "toggle help"}
        }),
        nav_up = btea.new_binding({
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        }),
        nav_down = btea.new_binding({
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        }),
        action1 = btea.new_binding({
            keys = {"1"},
            help = {key = "1", desc = "action one"}
        }),
        action2 = btea.new_binding({
            keys = {"2"},
            help = {key = "2", desc = "action two"}
        }),
        save = btea.new_binding({
            keys = {"ctrl+s"},
            help = {key = "^S", desc = "save"}
        }),
        reload = btea.new_binding({
            keys = {"ctrl+r"},
            help = {key = "^R", desc = "reload"}
        })
    }

    -- Create help component
    local help = btea.new_help({
        width = 60,
        styles = {
            short_key = btea.new_style():foreground("#89B4FA"):bold(),
            short_desc = btea.new_style():foreground("#CDD6F4"),
            short_separator = btea.new_style():foreground("#45475A"),
            full_key = btea.new_style():foreground("#89B4FA"):bold(),
            full_desc = btea.new_style():foreground("#CDD6F4"),
            full_separator = btea.new_style():foreground("#45475A")
        }
    })

    -- Define our help keymap using a table
    local keymap = {
        show_full = false,

        -- Return most important bindings for short help
        short_help = function(self)
            return {
                keys.quit,
                keys.toggle_help,
                keys.nav_up,
                keys.nav_down
            }
        end,

        -- Return all bindings grouped by category for full help
        full_help = function(self)
            return {
                -- Navigation group
                {
                    keys.nav_up,
                    keys.nav_down
                },
                -- Actions group
                {
                    keys.action1,
                    keys.action2
                },
                -- File operations group
                {
                    keys.save,
                    keys.reload
                },
                -- System group
                {
                    keys.toggle_help,
                    keys.quit
                }
            }
        end
    }

    -- Styles for demo layout
    local styles = {
        base = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.new_style()
            :foreground("#CBA6F7")
            :bold(),

        content = btea.new_style()
            :foreground("#CDD6F4"),

        action = btea.new_style()
            :foreground("#F9E2AF"):italic()
    }

    local last_action = "No action taken yet"

    local function create_view()
        local lines = {
            styles.title:render("Help Table Demo"),
            "",
            styles.content:render("This demo shows how to use help with a Lua table."),
            styles.content:render("Press '?' to toggle between short and full help views."),
            "",
            styles.action:render("Last action: " .. last_action),
            ""
        }

        -- Add help view
        help:set_show_all(keymap.show_full)
        table.insert(lines, help:view(keymap))

        return styles.base:render(table.concat(lines, "\n"))
    end

    -- Start alt screen and hide cursor
    cmd_channel:send(btea.batch({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }))

    coroutine.spawn(function()
        while true do
            local result = channel.select({
                cmd_channel:case_receive(),
                done:case_receive()
            })

            if result.channel == done then
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

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if type(msg) == "table" then
            if msg.type == "update" then
                if msg.key then
                    -- Handle key bindings
                    if keys.quit:matches(msg) then
                        last_action = "Quitting..."
                        break
                    elseif keys.toggle_help:matches(msg) then
                        keymap.show_full = not keymap.show_full
                        last_action = "Toggled help view"
                    elseif keys.nav_up:matches(msg) then
                        last_action = "Moved up"
                    elseif keys.nav_down:matches(msg) then
                        last_action = "Moved down"
                    elseif keys.action1:matches(msg) then
                        last_action = "Performed action one"
                    elseif keys.action2:matches(msg) then
                        last_action = "Performed action two"
                    elseif keys.save:matches(msg) then
                        last_action = "Saved"
                    elseif keys.reload:matches(msg) then
                        last_action = "Reloaded"
                    end
                end
                task:complete(create_view())
            elseif msg.type == "view" then
                task:complete(create_view())
            else
                task:complete("ok")
            end
        else
            task:complete("ok")
        end
    end

    -- Cleanup
    done:close()
    cmd_channel:send(btea.batch({
        btea.commands.show_cursor,
        btea.commands.exit_alt_screen
    }))
end

return App