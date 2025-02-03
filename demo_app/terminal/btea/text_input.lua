function App()
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Create different text inputs with various styles
    local inputs = {
        {
            name = "Default",
            input = btea.new_text_input({
                prompt = "> ",
                placeholder = "Basic input...",
                width = 40
            })
        },
        {
            name = "Password",
            input = btea.new_text_input({
                prompt = "🔒 ",
                placeholder = "Enter password...",
                echo_mode = btea.ECHO_PASSWORD,
                echo_character = "•",
                width = 40
            })
        },
        {
            name = "Limited",
            input = btea.new_text_input({
                prompt = "# ",
                placeholder = "Max 10 chars...",
                char_limit = 10,
                width = 40
            })
        },
        {
            name = "With Validation",
            input = btea.new_text_input({
                prompt = "$ ",
                placeholder = "Numbers only...",
                width = 40,
                validate = function(s)
                    if s:match("^%d*$") then
                        return nil
                    end
                    return "Only numbers allowed"
                end
            })
        },
        {
            name = "With Suggestions",
            input = btea.new_text_input({
                prompt = "cmd: ",
                placeholder = "Type command...",
                width = 40,
                show_suggestions = true,
                suggestions = {"help", "status", "quit", "clear", "restart"}
            })
        }
    }

    -- Define key bindings
    local keys = {
        next = btea.new_binding({
            keys = {"tab", "down"},
            help = {key = "tab/↓", desc = "next input"}
        }),
        prev = btea.new_binding({
            keys = {"shift+tab", "up"},
            help = {key = "shift+tab/↑", desc = "prev input"}
        }),
        quit = btea.new_binding({
            keys = {"ctrl+c", "esc"},
            help = {key = "^C/esc", desc = "quit"}
        })
    }

    -- Track current input
    local current = 1
    local results = {}

    -- Style definitions
    local styles = {
        base = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.new_style()
            :foreground("#CDD6F4")
            :bold(),

        label = btea.new_style()
            :foreground("#89B4FA"),

        result = btea.new_style()
            :foreground("#A6E3A1")
            :italic(),

        error = btea.new_style()
            :foreground("#F38BA8")
            :italic(),

        help = btea.new_style()
            :foreground("#6C7086")
            :italic()
    }

    -- Focus first input
    inputs[current].input:focus()

    local function create_view()
        local lines = {
            styles.title:render("Text Input Types Demo"),
            ""
        }

        for i, input in ipairs(inputs) do
            -- Add input label
            table.insert(lines, styles.label:render(input.name))
            -- Add the input itself
            table.insert(lines, input.input:view())
            -- Add result or error if any
            if results[i] then
                table.insert(lines, styles.result:render("→ " .. results[i]))
            end
            if not input.input:is_valid() then
                table.insert(lines, styles.error:render("✗ " .. input.input:error()))
            end
            table.insert(lines, "")
        end

        -- Add help text
        table.insert(lines, styles.help:render("Tab/↓ next | Shift+Tab/↑ prev | Enter submit | ^C/Esc quit"))

        return styles.base:render(table.concat(lines, "\n"))
    end

    -- Start alt screen and hide cursor
    cmd_channel:send(btea.batch({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }))

    -- Command processor
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
                -- Handle key presses
                if msg.key then
                    if keys.quit:matches(msg) then
                        break
                    elseif keys.next:matches(msg) then
                        -- Move to next input
                        inputs[current].input:blur()
                        current = (current % #inputs) + 1
                        local cmd = inputs[current].input:focus()
                        if cmd then cmd_channel:send(cmd) end
                    elseif keys.prev:matches(msg) then
                        -- Move to previous input
                        inputs[current].input:blur()
                        current = ((current - 2) % #inputs) + 1
                        local cmd = inputs[current].input:focus()
                        if cmd then cmd_channel:send(cmd) end
                    elseif msg.key.key_type == "enter" then
                        -- Store input result
                        results[current] = inputs[current].input:value()
                        inputs[current].input:set_value("")
                    end
                end

                -- Update current input
                local cmd = inputs[current].input:update(msg)
                if cmd then cmd_channel:send(cmd) end
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