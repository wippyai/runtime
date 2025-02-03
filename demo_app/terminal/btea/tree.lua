function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Define key bindings
    local keys = {
        quit = btea.new_binding({
            keys = {"q", "ctrl+c"},
            help = {key = "q/^C", desc = "quit"}
        }),
        cycle_style = btea.new_binding({
            keys = {"s"},
            help = {key = "s", desc = "cycle style"}
        }),
        toggle_hidden = btea.new_binding({
            keys = {"h"},
            help = {key = "h", desc = "toggle hidden"}
        })
    }

    -- Available style themes
    local themes = {
        {
            name = "Catppuccin",
            base = btea.new_style()
                :border(btea.borders.ROUNDED)
                :padding(1)
                :background("#1E1E2E"),
            directory = btea.new_style():foreground("#89B4FA"):bold(),
            file = btea.new_style():foreground("#CDD6F4"),
            config = btea.new_style():foreground("#F9E2AF"),
            executable = btea.new_style():foreground("#A6E3A1"),
            hidden = btea.new_style():foreground("#45475A"),
            title = btea.new_style():foreground("#CBA6F7"):bold(),
            help = btea.new_style():foreground("#6C7086"):italic()
        },
        {
            name = "Gruvbox",
            base = btea.new_style()
                :border(btea.borders.ROUNDED)
                :padding(1)
                :background("#282828"),
            directory = btea.new_style():foreground("#83A598"):bold(),
            file = btea.new_style():foreground("#EBDBB2"),
            config = btea.new_style():foreground("#FABD2F"),
            executable = btea.new_style():foreground("#B8BB26"),
            hidden = btea.new_style():foreground("#928374"),
            title = btea.new_style():foreground("#D3869B"):bold(),
            help = btea.new_style():foreground("#665C54"):italic()
        },
        {
            name = "Nord",
            base = btea.new_style()
                :border(btea.borders.ROUNDED)
                :padding(1)
                :background("#2E3440"),
            directory = btea.new_style():foreground("#88C0D0"):bold(),
            file = btea.new_style():foreground("#D8DEE9"),
            config = btea.new_style():foreground("#EBCB8B"),
            executable = btea.new_style():foreground("#A3BE8C"),
            hidden = btea.new_style():foreground("#4C566A"),
            title = btea.new_style():foreground("#B48EAD"):bold(),
            help = btea.new_style():foreground("#616E88"):italic()
        }
    }

    -- State
    local state = {
        current_theme = 1,
        show_hidden = false,
        root_path = lfs.currentdir()
    }

    -- Create tree from filesystem
    local function create_fs_tree(path, depth)
        depth = depth or 0
        if depth > 3 then return nil end -- Limit recursion depth

        local tree = btea.new_tree():root(path:match("([^/\\]+)$"))

        for file in lfs.dir(path) do
            if file ~= "." and file ~= ".." then
                local full_path = path .. "/" .. file
                local attr = lfs.attributes(full_path)

                if attr then
                    -- Skip hidden files if not showing them
                    if not state.show_hidden and file:match("^%.") then
                        goto continue
                    end

                    if attr.mode == "directory" then
                        local subtree = create_fs_tree(full_path, depth + 1)
                        if subtree then
                            tree:child(subtree)
                        end
                    else
                        tree:child(file)
                    end
                end
                ::continue::
            end
        end

        return tree
    end

    -- Style the tree based on file types
    local function style_node(children, index)
        local theme = themes[state.current_theme]
        local node = children:at(index)
        if not node then return theme.file end

        local value = node:value()
        local full_path = state.root_path .. "/" .. value
        local attr = lfs.attributes(full_path)

        if attr then
            if attr.mode == "directory" then
                return theme.directory
            elseif value:match("^%.") then
                return theme.hidden
            elseif value:match("%.json$") or value:match("%.toml$") or
                   value:match("%.yaml$") or value:match("%.ini$") or
                   value:match("%.env") then
                return theme.config
            elseif attr.mode == "file" and attr.permissions:match("x") then
                return theme.executable
            else
                return theme.file
            end
        end

        return theme.file
    end

    -- Initialize tree
    local tree = create_fs_tree(state.root_path)
    tree:item_style_func(style_node)
        :enumerator(btea.enumerators.ROUNDED)

    -- Create view
    local function create_view()
        local theme = themes[state.current_theme]
        local lines = {
            theme.title:render("File Tree Browser - " .. theme.name),
            theme.title:render("Path: " .. state.root_path),
            ""
        }

        -- Add tree view
        table.insert(lines, tree:view())
        table.insert(lines, "")

        -- Add help text
        table.insert(lines, theme.help:render(
            "s: cycle theme • h: toggle hidden • q/^C: quit"
        ))

        return theme.base:render(table.concat(lines, "\n"))
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
                if msg.key then
                    if keys.quit:matches(msg) then
                        break
                    elseif keys.cycle_style:matches(msg) then
                        -- Cycle through themes
                        state.current_theme = (state.current_theme % #themes) + 1
                        -- Update tree styling
                        tree:item_style_func(style_node)
                    elseif keys.toggle_hidden:matches(msg) then
                        state.show_hidden = not state.show_hidden
                        -- Recreate tree with new hidden files setting
                        tree = create_fs_tree(state.root_path)
                        tree:item_style_func(style_node)
                            :enumerator(btea.enumerators.ROUNDED)
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