-- Norton Commander-style dual-pane file manager demo.
-- Showcases tty module: styles, borders, layout, key bindings, input events.

local io = require("io")
local tty = require("tty")

local LEFT = tty.text.position.LEFT
local CENTER = tty.text.position.CENTER
local RIGHT = tty.text.position.RIGHT
local TOP = tty.text.position.TOP

local DOUBLE = tty.borders.DOUBLE

-- ANSI sequences
local ESC = "\027["
local ALT_SCREEN_ON = "\027[?1049h"
local ALT_SCREEN_OFF = "\027[?1049l"
local CURSOR_HIDE = ESC .. "?25l"
local CURSOR_SHOW = ESC .. "?25h"
local CURSOR_HOME = ESC .. "H"
local CLEAR_BELOW = ESC .. "J"

-- Simulated filesystem
type FsEntry = {name: string, size: integer, kind: string, date: string}

local fs_tree: {[string]: {FsEntry}} = {
    ["/"] = {
        {name = "..",     size = 0,     kind = "up",  date = "2026-01-01 00:00"},
        {name = "bin",    size = 4096,  kind = "dir", date = "2026-02-10 08:30"},
        {name = "etc",    size = 4096,  kind = "dir", date = "2026-02-14 11:20"},
        {name = "home",   size = 4096,  kind = "dir", date = "2026-01-28 09:45"},
        {name = "usr",    size = 4096,  kind = "dir", date = "2026-02-01 14:00"},
        {name = "var",    size = 4096,  kind = "dir", date = "2026-02-12 16:30"},
        {name = "tmp",    size = 4096,  kind = "dir", date = "2026-02-16 10:00"},
        {name = "LICENSE",      size = 1071,   kind = "file", date = "2025-06-15 12:00"},
        {name = "README.md",    size = 3420,   kind = "file", date = "2026-02-10 09:15"},
        {name = "Makefile",     size = 2180,   kind = "file", date = "2026-01-20 17:30"},
    },
    ["/home"] = {
        {name = "..",           size = 0,      kind = "up",   date = "2026-01-01 00:00"},
        {name = "user",         size = 4096,   kind = "dir",  date = "2026-02-15 20:00"},
        {name = "guest",        size = 4096,   kind = "dir",  date = "2026-01-10 08:00"},
        {name = ".bashrc",      size = 420,    kind = "file", date = "2026-02-01 10:00"},
        {name = ".profile",     size = 180,    kind = "file", date = "2025-12-01 09:00"},
    },
    ["/home/user"] = {
        {name = "..",           size = 0,      kind = "up",   date = "2026-01-01 00:00"},
        {name = "Documents",    size = 4096,   kind = "dir",  date = "2026-02-14 18:00"},
        {name = "Downloads",    size = 4096,   kind = "dir",  date = "2026-02-16 09:30"},
        {name = "projects",     size = 4096,   kind = "dir",  date = "2026-02-15 21:00"},
        {name = ".gitconfig",   size = 256,    kind = "file", date = "2026-01-05 11:00"},
        {name = ".vimrc",       size = 1420,   kind = "file", date = "2026-02-08 14:30"},
        {name = "notes.txt",    size = 8320,   kind = "file", date = "2026-02-16 08:45"},
        {name = "todo.md",      size = 512,    kind = "file", date = "2026-02-15 19:00"},
    },
    ["/home/user/projects"] = {
        {name = "..",           size = 0,      kind = "up",   date = "2026-01-01 00:00"},
        {name = "wippy",        size = 4096,   kind = "dir",  date = "2026-02-16 10:00"},
        {name = "scripts",      size = 4096,   kind = "dir",  date = "2026-02-10 15:00"},
        {name = "go.mod",       size = 890,    kind = "file", date = "2026-02-16 09:00"},
        {name = "go.sum",       size = 45200,  kind = "file", date = "2026-02-16 09:00"},
        {name = "main.go",      size = 2340,   kind = "file", date = "2026-02-15 22:00"},
        {name = "config.yaml",  size = 680,    kind = "file", date = "2026-02-14 16:00"},
        {name = "Dockerfile",   size = 420,    kind = "file", date = "2026-02-12 11:30"},
        {name = "Makefile",     size = 1560,   kind = "file", date = "2026-02-13 14:00"},
        {name = "README.md",    size = 5120,   kind = "file", date = "2026-02-15 20:00"},
    },
    ["/etc"] = {
        {name = "..",           size = 0,      kind = "up",   date = "2026-01-01 00:00"},
        {name = "nginx",        size = 4096,   kind = "dir",  date = "2026-02-10 12:00"},
        {name = "ssh",          size = 4096,   kind = "dir",  date = "2026-01-15 08:00"},
        {name = "hosts",        size = 340,    kind = "file", date = "2025-11-01 00:00"},
        {name = "passwd",       size = 2100,   kind = "file", date = "2026-02-01 10:00"},
        {name = "resolv.conf",  size = 128,    kind = "file", date = "2026-02-10 08:00"},
        {name = "fstab",        size = 560,    kind = "file", date = "2025-10-15 12:00"},
    },
    ["/usr"] = {
        {name = "..",           size = 0,      kind = "up",   date = "2026-01-01 00:00"},
        {name = "bin",          size = 4096,   kind = "dir",  date = "2026-02-10 08:30"},
        {name = "lib",          size = 4096,   kind = "dir",  date = "2026-02-10 08:30"},
        {name = "local",        size = 4096,   kind = "dir",  date = "2026-01-20 14:00"},
        {name = "share",        size = 4096,   kind = "dir",  date = "2026-02-05 16:00"},
    },
}

local default_listing: {FsEntry} = {
    {name = "..", size = 0, kind = "up", date = "2026-01-01 00:00"},
}

local function get_listing(path: string): {FsEntry}
    return fs_tree[path] or default_listing
end

local function parent_path(path)
    if path == "/" then return "/" end
    local parent = path:match("^(.+)/[^/]+$")
    return parent or "/"
end

local function join_path(base, name)
    if base == "/" then return "/" .. name end
    return base .. "/" .. name
end

local function format_size(bytes)
    if bytes >= 1048576 then
        return string.format("%.1fM", bytes / 1048576)
    elseif bytes >= 1024 then
        return string.format("%.1fK", bytes / 1024)
    end
    return tostring(bytes)
end

-- Key bindings
local keys = {
    quit = tty.bind({
        keys = {"f10", "ctrl+c"},
        help = {key = "F10", desc = "quit"},
    }),
    nav_up = tty.bind({
        keys = {"up", "k"},
        help = {key = "up/k", desc = "up"},
    }),
    nav_down = tty.bind({
        keys = {"down", "j"},
        help = {key = "dn/j", desc = "down"},
    }),
    enter = tty.bind({
        keys = {"enter"},
        help = {key = "enter", desc = "open"},
    }),
    switch_panel = tty.bind({
        keys = {"tab"},
        help = {key = "tab", desc = "switch"},
    }),
    page_up = tty.bind({
        keys = {"pgup"},
        help = {key = "pgup", desc = "page up"},
    }),
    page_down = tty.bind({
        keys = {"pgdown"},
        help = {key = "pgdn", desc = "page down"},
    }),
    go_home = tty.bind({
        keys = {"home"},
        help = {key = "home", desc = "first"},
    }),
    go_end = tty.bind({
        keys = {"end"},
        help = {key = "end", desc = "last"},
    }),
}

-- Panel state
type Panel = {path: string, cursor: integer, scroll: integer, listing: {FsEntry}}

local function new_panel(path: string): Panel
    return {
        path = path,
        cursor = 1,
        scroll = 0,
        listing = get_listing(path),
    }
end

-- State
local width, height = 80, 24
local running = true
local active_panel = 1
local panels = {
    new_panel("/home/user"),
    new_panel("/home/user/projects"),
}

-- Styles
local nc_blue = "#000088"
local nc_cyan = "#00AAAA"

local s = {
    panel_border   = tty.style():foreground(nc_cyan):background(nc_blue),
    panel_title    = tty.style():foreground("#FFFFFF"):background(nc_blue):bold(),
    file_normal    = tty.style():foreground(nc_cyan):background(nc_blue),
    file_dir       = tty.style():foreground("#FFFFFF"):background(nc_blue):bold(),
    file_exec      = tty.style():foreground("#00CC00"):background(nc_blue),
    file_selected  = tty.style():foreground("#000000"):background(nc_cyan),
    file_dir_sel   = tty.style():foreground("#000000"):background(nc_cyan):bold(),
    header_bg      = tty.style():background(nc_blue):foreground(nc_cyan),
    status_bar     = tty.style():foreground("#000000"):background(nc_cyan),
    fn_key_num     = tty.style():foreground("#000000"):background(nc_cyan):bold(),
    fn_key_label   = tty.style():foreground("#000000"):background(nc_cyan),
    fn_key_gap     = tty.style():foreground(nc_cyan):background(nc_blue),
    info_bar       = tty.style():foreground(nc_cyan):background(nc_blue),
    menu_bar       = tty.style():foreground("#000000"):background(nc_cyan),
    menu_item      = tty.style():foreground("#000000"):background(nc_cyan),
    menu_hotkey    = tty.style():foreground("#FF0000"):background(nc_cyan):bold(),
}

-- Navigation
local function panel_enter(panel: Panel)
    local entry: FsEntry? = panel.listing[panel.cursor]
    if not entry then return end
    local target
    if entry.kind == "up" then
        target = parent_path(panel.path)
    elseif entry.kind == "dir" then
        target = join_path(panel.path, entry.name)
    else
        return
    end
    panel.path = target
    panel.listing = get_listing(target)
    panel.cursor = 1
    panel.scroll = 0
end

local function panel_move(panel: Panel, delta: number, page_h: number)
    local count = #panel.listing
    if count == 0 then return end
    panel.cursor = panel.cursor + delta
    if panel.cursor < 1 then panel.cursor = 1 end
    if panel.cursor > count then panel.cursor = count end
    if panel.cursor < panel.scroll + 1 then
        panel.scroll = panel.cursor - 1
    end
    if panel.cursor > panel.scroll + page_h then
        panel.scroll = panel.cursor - page_h
    end
end

-- Rendering
local function render_panel(panel: Panel, pw: integer, ph: integer, is_active: boolean): string
    local inner_w = pw - 2
    local list_h = ph - 5

    -- Path header
    local title_style = is_active and s.panel_title or s.file_normal
    local path_line = title_style:width(inner_w):align(tty.align.CENTER):render(panel.path)

    -- Column header
    local name_col_w = inner_w - 16
    if name_col_w < 8 then name_col_w = 8 end
    local hdr_line = tty.style():foreground("#FFCC00"):background(nc_blue):bold()
        :width(inner_w)
        :render("Name" .. string.rep(" ", name_col_w - 4)
            .. string.format("%6s", "Size") .. "  Date")

    -- File lines
    local lines = {}
    for i = panel.scroll + 1, math.min(panel.scroll + list_h, #panel.listing) do
        local entry: FsEntry? = panel.listing[i]
        if not entry then break end
        local is_cursor = (i == panel.cursor)

        local display_name = entry.name
        if entry.kind == "dir" then
            display_name = "/" .. display_name
        elseif entry.kind == "up" then
            display_name = "/.."
        end

        local size_str = entry.kind == "dir" and "<DIR>" or format_size(entry.size)
        if entry.kind == "up" then size_str = "UP" end
        local date_str = entry.date:sub(6, 10)

        local padded_name = display_name
        if #padded_name > name_col_w then
            padded_name = padded_name:sub(1, name_col_w - 1) .. "~"
        end
        padded_name = padded_name .. string.rep(" ", name_col_w - #padded_name)

        local row = padded_name .. string.format("%6s", size_str) .. "  " .. date_str

        local row_style
        if is_cursor and is_active then
            row_style = (entry.kind == "dir" or entry.kind == "up") and s.file_dir_sel or s.file_selected
        elseif entry.kind == "dir" or entry.kind == "up" then
            row_style = s.file_dir
        else
            row_style = s.file_normal
        end
        lines[#lines + 1] = row_style:width(inner_w):render(row)
    end

    -- Pad remaining lines
    local empty_line = s.file_normal:width(inner_w):render("")
    for _ = #lines + 1, list_h do
        lines[#lines + 1] = empty_line
    end

    -- Info line
    local entry: FsEntry? = panel.listing[panel.cursor]
    local info = ""
    if entry then
        info = entry.name
        if entry.kind == "file" then
            info = info .. "  " .. format_size(entry.size) .. "  " .. entry.date
        end
    end
    local info_line = s.info_bar:width(inner_w):render(info)

    -- Compose body and wrap with border
    local body = tty.text.join_vertical(LEFT,
        path_line, hdr_line, table.concat(lines, "\n"), info_line)

    local border_fg = is_active and "#FFFFFF" or nc_cyan
    return tty.style()
        :foreground(nc_cyan)
        :background(nc_blue)
        :border(DOUBLE)
        :border_foreground(border_fg)
        :border_background(nc_blue)
        :width(inner_w)
        :render(body)
end

local function render_fn_bar(w: integer): string
    local fn_items = {
        {num = "1", label = "Help"},
        {num = "2", label = "Menu"},
        {num = "3", label = "View"},
        {num = "4", label = "Edit"},
        {num = "5", label = "Copy"},
        {num = "6", label = "Move"},
        {num = "7", label = "Mkdir"},
        {num = "8", label = "Del"},
        {num = "9", label = "Pull"},
        {num = "10", label = "Quit"},
    }
    local parts = {}
    local item_w = math.floor(w / #fn_items)
    for _, f in ipairs(fn_items) do
        local label_w = item_w - #f.num
        if label_w < 1 then label_w = 1 end
        local label = f.label
        if #label > label_w then label = label:sub(1, label_w) end
        label = label .. string.rep(" ", label_w - #label)
        parts[#parts + 1] = s.fn_key_num:render(f.num) .. s.fn_key_label:render(label)
    end
    return table.concat(parts, "")
end

local function render_menu_bar(w: integer): string
    local items = {
        {hot = "L", rest = "eft"},
        {hot = "F", rest = "ile"},
        {hot = "C", rest = "ommand"},
        {hot = "O", rest = "ptions"},
        {hot = "R", rest = "ight"},
    }
    local parts = {}
    for _, item in ipairs(items) do
        parts[#parts + 1] = " " .. s.menu_hotkey:render(item.hot) .. s.menu_item:render(item.rest)
    end
    local menu_content = table.concat(parts, "")
    local menu_w = tty.text.width(menu_content)
    local pad = w - menu_w
    if pad < 0 then pad = 0 end
    return tty.style():foreground("#000000"):background(nc_cyan):width(w):render(menu_content .. string.rep(" ", pad))
end

local function render()
    local pw = math.floor(width / 2)
    local panel_h = height - 3

    local left = render_panel(panels[1], pw, panel_h, active_panel == 1)
    local right = render_panel(panels[2], width - pw, panel_h, active_panel == 2)
    local dual = tty.text.join_horizontal(TOP, left, right)

    local menu = render_menu_bar(width)
    local fn_bar = render_fn_bar(width)

    -- Prompt line
    local prompt = s.info_bar:width(width):render(panels[active_panel].path .. ">")

    local frame = tty.text.join_vertical(LEFT, menu, dual, prompt, fn_bar)
    io.write(CURSOR_HOME .. frame .. CLEAR_BELOW)
    io.flush()
end

-- Event handling
local function handle_key(event: any)
    local panel: Panel? = panels[active_panel]
    if not panel then return end
    local list_h = height - 8

    if keys.quit:matches(event) then
        running = false
    elseif keys.switch_panel:matches(event) then
        active_panel = active_panel == 1 and 2 or 1
    elseif keys.nav_up:matches(event) then
        panel_move(panel, -1, list_h)
    elseif keys.nav_down:matches(event) then
        panel_move(panel, 1, list_h)
    elseif keys.enter:matches(event) then
        panel_enter(panel)
    elseif keys.page_up:matches(event) then
        panel_move(panel, -list_h, list_h)
    elseif keys.page_down:matches(event) then
        panel_move(panel, list_h, list_h)
    elseif keys.go_home:matches(event) then
        panel_move(panel, -#panel.listing, list_h)
    elseif keys.go_end:matches(event) then
        panel_move(panel, #panel.listing, list_h)
    end
end

local function handle_event(event: any)
    if event.type == "key" and event.action ~= "release" then
        handle_key(event)
    elseif event.type == "resize" or event.type == "start" then
        width = event.width
        height = event.height
    end
end

-- Main
local function main()
    local ok, err = tty.start()
    if not ok then
        io.print("tty.start failed: " .. tostring(err))
        return
    end

    local cols, rows
    cols, rows, err = tty.screen_size()
    if cols then
        width = cols
        height = rows
    end

    local ch = tty.events()
    if not ch then
        tty.stop()
        io.print("tty.events failed")
        return
    end

    io.write(ALT_SCREEN_ON .. CURSOR_HIDE)
    io.flush()
    render()

    while running do
        local event = ch:receive()
        if event == nil then break end
        handle_event(event)
        if running then render() end
    end

    io.write(CURSOR_SHOW .. ALT_SCREEN_OFF)
    io.flush()
    tty.stop()
end

return main
