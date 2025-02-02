function App()
    local inbox = tasks.channel()
    local window_width = 80
    local window_height = 24

    -- State management
    local path_stack = {}  -- Stack to track directory history
    local current_path = lfs.currentdir()
    local selected_index = 1
    local entries = {}  -- Current directory entries

    -- Create styles
    local styles = {
        box = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.new_style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1),

        path = btea.new_style()
            :foreground("#6C7086"),

        tree = {
            root = btea.new_style()
                :bold()
                :foreground("#F5C2E7"),

            item = btea.new_style()
                :foreground("#A6E3A1"),

            enumerator = btea.new_style()
                :foreground("#89B4FA"),

            folder = btea.new_style()
                :foreground("#F5C2E7")
                :bold(),

            selected = btea.new_style()
                :background("#45475A")
                :foreground("#FFE55C")
                :bold(),

            selected_indicator = btea.new_style()
                :foreground("#FFE55C")
                :bold()
        }
    }

    -- Function to read directory contents
    local function read_directory(path)
        entries = {}
        local files = {}
        local folders = {}

        for entry in lfs.dir(path) do
            if entry ~= "." and entry ~= ".git" then
                local full_path = path .. "/" .. entry
                local attr = lfs.attributes(full_path)
                if attr then
                    if attr.mode == "directory" then
                        table.insert(folders, {name = entry, is_dir = true})
                    else
                        table.insert(files, {name = entry, is_dir = false})
                    end
                end
            end
        end

        table.sort(folders, function(a, b) return a.name < b.name end)
        table.sort(files, function(a, b) return a.name < b.name end)

        -- Add parent directory if not at root
        if path ~= "/" then
            table.insert(entries, {name = "..", is_dir = true})
        end

        -- Combine folders and files
        for _, folder in ipairs(folders) do
            table.insert(entries, folder)
        end
        for _, file in ipairs(files) do
            table.insert(entries, file)
        end

        selected_index = 1
        return entries
    end

    -- Build tree for current directory
    local function build_current_tree()
        local tree = btea.new_tree()
        tree:root(current_path)
        tree:root_style(styles.tree.root)
        tree:enumerator_style(styles.tree.enumerator)

        for i, entry in ipairs(entries) do
            local display_name = entry.name
            if i == selected_index then
                -- Create selected tree node with arrow indicator
                local selected_tree = btea.new_tree()
                selected_tree:root("→ " .. display_name .. (entry.is_dir and "/" or ""))
                selected_tree:root_style(styles.tree.selected)
                tree:child(selected_tree)
            else
                -- Add regular node with appropriate style
                if entry.is_dir then
                    local dir_tree = btea.new_tree()
                    dir_tree:root(display_name .. "/")
                    dir_tree:root_style(styles.tree.folder)
                    tree:child(dir_tree)
                else
                    tree:child(display_name)
                end
            end
        end

        return tree
    end

    -- Handle input
    local function handle_input(msg)
        if msg.key then
            if msg.key.key_type == "up" and selected_index > 1 then
                selected_index = selected_index - 1
                return true
            elseif msg.key.key_type == "down" and selected_index < #entries then
                selected_index = selected_index + 1
                return true
            elseif msg.key.key_type == "enter" then
                local selected = entries[selected_index]
                if selected and selected.is_dir then
                    if selected.name == ".." then
                        -- Go to parent directory
                        if #path_stack > 0 then
                            current_path = table.remove(path_stack)
                        end
                    else
                        -- Go into directory
                        table.insert(path_stack, current_path)
                        current_path = current_path .. "/" .. selected.name
                    end
                    read_directory(current_path)
                    return true
                end
            elseif msg.key.key_type == "esc" and #path_stack > 0 then
                -- Go back on escape
                current_path = table.remove(path_stack)
                read_directory(current_path)
                return true
            end
        end
        return false
    end

    -- Initialize directory listing
    read_directory(current_path)

    -- Main application loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            break
        end

        local msg = task:input()

        if type(msg) == "table" then
            if msg.type == "update" then
                -- Handle window resize
                if msg.window_size then
                    window_width = msg.window_size.width
                    window_height = msg.window_size.height
                end

                -- Handle input
                if handle_input(msg) then
                    task:complete("redraw")
                else
                    task:complete("ok")
                end
            elseif msg.type == "view" then
                -- Create help text
                local help = {
                    "↑/↓: Navigate",
                    "Enter: Open directory",
                    "Esc: Go back"
                }
                local help_text = styles.tree.item:render(table.concat(help, " | "))

                -- Selected item info
                local selected = entries[selected_index]
                local selected_info = ""
                if selected then
                    selected_info = styles.path:render(
                        "Selected: " ..
                        (selected.is_dir and "Directory" or "File") ..
                        " - " .. selected.name
                    )
                end

                -- Create box with header and tree view
                local box_content = {
                    styles.header:render("Directory Browser"),
                    styles.path:render("Path: " .. current_path),
                    "",
                    build_current_tree():view(),
                    "",
                    selected_info,
                    help_text
                }

                local view = styles.box
                    :width(window_width - 2)
                    :height(window_height - 2)
                    :render(table.concat(box_content, "\n"))

                task:complete(view)
            end
        end
    end
end

return App