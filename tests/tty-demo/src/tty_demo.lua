-- SPDX-License-Identifier: MPL-2.0

-- TTY Toolkit Interactive Demo
-- Full-screen TUI exercising every function in the tty module.

local io = require("io")
local tty = require("tty")

local LEFT = tty.text.position.LEFT
local CENTER = tty.text.position.CENTER
local TOP = tty.text.position.TOP
local RIGHT = tty.text.position.RIGHT

local ROUNDED = tty.borders.ROUNDED
local NORMAL = tty.borders.NORMAL
local THICK = tty.borders.THICK
local DOUBLE = tty.borders.DOUBLE
local HIDDEN = tty.borders.HIDDEN

-- ANSI sequences
local ESC = "\027["
local ALT_SCREEN_ON = "\027[?1049h"
local ALT_SCREEN_OFF = "\027[?1049l"
local CURSOR_HIDE = ESC .. "?25l"
local CURSOR_SHOW = ESC .. "?25h"
local CURSOR_HOME = ESC .. "H"
local CLEAR_BELOW = ESC .. "J"

-- Reusable styles
local theme = {
    title    = tty.style():foreground("#FFCC00"):bold(),
    label    = tty.style():foreground("#888888"),
    value    = tty.style():foreground("#FFFFFF"):bold(),
    ok       = tty.style():foreground("#00CC00"):bold(),
    fail     = tty.style():foreground("#FF0000"):bold(),
    muted    = tty.style():foreground("#555555"):italic(),
    section  = tty.style():foreground("#AAAAAA"):bold(),
    header   = tty.style():bold():foreground("#FFFFFF"):background("#FF6600"),
    status   = tty.style():background("#222222"),
}

-- Key bindings
local bindings = {
    quit = tty.bind({
        keys = {"q", "ctrl+c", "esc"},
        help = {key = "q/esc", desc = "quit"},
    }),
    next_tab = tty.bind({
        keys = {"tab", "l"},
        help = {key = "tab/l", desc = "next tab"},
    }),
    prev_tab = tty.bind({
        keys = {"shift+tab", "h"},
        help = {key = "S-tab/h", desc = "prev tab"},
    }),
}

-- State
local width, height = 80, 24
local events = {}
local max_events = 100
local mouse_x, mouse_y = 0, 0
local key_count, mouse_count, resize_count = 0, 0, 0
local running = true
local tab_index = 1
local tab_names = {"Events", "Styles", "Layout", "Bindings"}

local function push_event(ev: any)
    table.insert(events, 1, ev)
    if #events > max_events then
        table.remove(events)
    end
end

local function format_event(ev)
    if ev.type == "key" then
        local mods = ""
        if ev.ctrl then mods = mods .. "ctrl+" end
        if ev.alt then mods = mods .. "alt+" end
        if ev.shift then mods = mods .. "shift+" end
        local k = ev.key_type ~= "runes" and ev.key_type or ev.key
        local act = ev.action == "release" and " up" or ""
        return "KEY " .. mods .. k .. act
    elseif ev.type == "mouse" then
        return "MOUSE " .. ev.action .. " " .. ev.button .. " @" .. ev.x .. "," .. ev.y
    elseif ev.type == "resize" then
        return "RESIZE " .. ev.width .. "x" .. ev.height
    elseif ev.type == "focus" then
        return "FOCUS " .. (ev.focused and "in" or "out")
    elseif ev.type == "start" then
        return "START " .. ev.width .. "x" .. ev.height
    end
    return ev.type
end

-- Tab: Events

local function render_events_tab(w: integer, h: integer): string
    local title = theme.title:copy():width(w):align(tty.align.CENTER):render("Live Event Stream")
    local lines = {}
    local visible = h - 3
    for i = 1, math.min(#events, visible) do
        local s = tty.style():foreground(i == 1 and "#00FF88" or "#666666")
        if i == 1 then s = s:bold() end
        lines[#lines + 1] = s:max_width(w):render("  " .. format_event(events[i]))
    end
    if #events == 0 then
        lines[#lines + 1] = theme.muted:render("  waiting for events...")
    end
    return tty.text.join_vertical(LEFT, title, "", table.concat(lines, "\n"))
end

-- Tab: Styles

local function render_styles_tab(w: integer, h: integer): string
    local lbl = theme.label
    local title = theme.title:copy():width(w):align(tty.align.CENTER):render("Style Showcase")

    local fmt_line =
        tty.style():bold():render("bold") .. "  " ..
        tty.style():italic():render("italic") .. "  " ..
        tty.style():underline():render("underline") .. "  " ..
        tty.style():strikethrough():render("strike") .. "  " ..
        tty.style():faint():render("faint") .. "  " ..
        tty.style():reverse():render("reverse")

    local colors = {"#FF0000", "#FF6600", "#FFCC00", "#00CC00", "#0066FF", "#6600CC", "#CC00CC"}
    local fg_parts, bg_parts = {}, {}
    for _, c in ipairs(colors) do
        fg_parts[#fg_parts + 1] = tty.style():foreground(c):bold():render("@@")
        bg_parts[#bg_parts + 1] = tty.style():background(c):foreground("#000000"):render("  ")
    end
    local fg_line = table.concat(fg_parts, " ")
    local bg_line = table.concat(bg_parts, " ")

    local bdr = tty.style():border_foreground("#666666"):foreground("#CCCCCC"):padding(0, 1)
    local b_normal = bdr:copy():border(NORMAL):render(NORMAL)
    local b_rounded = bdr:copy():border(ROUNDED):render(ROUNDED)
    local b_thick = bdr:copy():border(THICK):render(THICK)
    local b_double = bdr:copy():border(DOUBLE):render(DOUBLE)
    local b_hidden = bdr:copy():border(HIDDEN):render(HIDDEN)
    local border_row = tty.text.join_horizontal(CENTER, b_normal, b_rounded, b_thick)
    local border_row2 = tty.text.join_horizontal(CENTER, b_double, b_hidden)

    local partial = tty.style()
        :border(ROUNDED, true, false, true, false)
        :border_foreground("#FF6600"):border_background("#330000")
        :padding(0, 1):render("top+bottom only")

    local padded = tty.style()
        :foreground("#00CC00"):background("#003300")
        :padding(0, 2):render("padded")
    local margined = tty.style()
        :foreground("#0066FF"):margin(0, 1):render("margin")

    local fixed_w = tty.style():width(20):foreground("#FFCC00"):render("width=20")
    local max_w = tty.style():max_width(15):foreground("#00CCCC"):render("max_width=15 long")

    local align_box = tty.text.join_vertical(LEFT,
        tty.style():width(24):align(tty.align.LEFT):foreground("#888888"):render("left"),
        tty.style():width(24):align(tty.align.CENTER):foreground("#CCCCCC"):render("center"),
        tty.style():width(24):align(tty.align.RIGHT):foreground("#FFFFFF"):render("right"))

    local inline_s = tty.style():inline():foreground("#00CC00"):render("inline removes\nnewlines")

    local base = tty.style():foreground("#00CC00"):bold()
    local derived = base:copy():foreground("#FF0000")
    local copy_line = base:render("original") .. "  " .. derived:render("copy")

    local multi = tty.style():foreground("#FFCC00"):render("hello", " ", "world", "!")

    return tty.text.join_vertical(LEFT,
        title, "",
        "  " .. lbl:render("Formatting: ") .. fmt_line,
        "  " .. lbl:render("FG colors:  ") .. fg_line,
        "  " .. lbl:render("BG colors:  ") .. bg_line,
        "  " .. lbl:render("Borders:"),
        border_row,
        border_row2,
        "  " .. lbl:render("Partial:"),
        partial,
        "  " .. lbl:render("Spacing:    ") .. padded .. margined,
        "  " .. lbl:render("Sizing:     ") .. fixed_w .. "  " .. max_w,
        "  " .. lbl:render("Alignment:"),
        align_box,
        "  " .. lbl:render("Inline:     ") .. inline_s,
        "  " .. lbl:render("Copy:       ") .. copy_line,
        "  " .. lbl:render("Multi-arg:  ") .. multi)
end

-- Tab: Layout

local function render_layout_tab(w: integer, h: integer): string
    local lbl = theme.label
    local val = theme.value
    local title = theme.title:copy():width(w):align(tty.align.CENTER):render("Text Layout & Placement")

    local sample = "Hello, World!"
    local tw, th = tty.text.size("line1\nline2!")

    local box_style = tty.style():border(ROUNDED):padding(0, 1)
    local box_a = box_style:copy():foreground("#FF6600"):render("A")
    local box_b = box_style:copy():foreground("#00CC00"):render("B")
    local box_c = box_style:copy():foreground("#0066FF"):render("C")

    local horiz = tty.text.join_horizontal(CENTER, box_a, box_b, box_c)
    local vert = tty.text.join_vertical(CENTER, box_a, box_b)
    local joined = tty.text.join_horizontal(TOP, horiz, "   ", vert)

    local pw = math.min(30, w - 4)
    local placed = tty.text.place(pw, 3, CENTER, CENTER, "centered")
    local placed_box = tty.style():border(ROUNDED):border_foreground("#444444"):render(placed)

    local ph = tty.text.place_horizontal(pw, RIGHT, "right-aligned")

    local valign_box = tty.style()
        :width(16):height(3)
        :align(tty.align.CENTER):align_vertical(tty.align.CENTER)
        :foreground("#FF6600"):border(ROUNDED):border_foreground("#333333")
        :render("v-center")

    return tty.text.join_vertical(LEFT,
        title, "",
        "  " .. lbl:render("width:      ") .. val:render(tostring(tty.text.width(sample))),
        "  " .. lbl:render("height:     ") .. val:render(tostring(tty.text.height("a\nb\nc"))),
        "  " .. lbl:render("size:       ") .. val:render(tw .. "x" .. th),
        "  " .. lbl:render("max_width:  ") .. val:render(tostring(tty.text.max_width({"short", "a longer string", "mid"}))),
        "  " .. lbl:render("max_height: ") .. val:render(tostring(tty.text.max_height({"one", "one\ntwo\nthree", "a"}))),
        "",
        "  " .. lbl:render("join_horizontal + join_vertical:"),
        joined,
        "",
        "  " .. lbl:render("place (centered in box):"),
        placed_box,
        "",
        "  " .. lbl:render("place_horizontal:"),
        "  |" .. ph .. "|",
        "",
        "  " .. lbl:render("align_vertical:"),
        valign_box)
end

-- Tab: Bindings

local function render_bindings_tab(w: integer, h: integer): string
    local lbl = theme.label
    local val = theme.value
    local sec = theme.section
    local title = theme.title:copy():width(w):align(tty.align.CENTER):render("Key Bindings")

    local help = bindings.quit:help()
    local lines = {
        "  " .. lbl:render("quit:    ") .. val:render(tostring(bindings.quit)),
        "  " .. lbl:render("  key:   ") .. val:render(help.key),
        "  " .. lbl:render("  desc:  ") .. val:render(help.desc),
        "  " .. lbl:render("  on:    ") .. val:render(tostring(bindings.quit:is_enabled())),
        "",
        "  " .. lbl:render("tab:     ") .. val:render(tostring(bindings.next_tab)),
        "",
    }

    lines[#lines + 1] = "  " .. sec:render("Match tests:")
    local tests = {
        {name = "q",          ev = {type = "key", key = "q",   key_type = "runes", ctrl = false, alt = false, shift = false}},
        {name = "ctrl+c",     ev = {type = "key", key = "c",   key_type = "runes", ctrl = true,  alt = false, shift = false}},
        {name = "esc",        ev = {type = "key", key = "esc", key_type = "esc",   ctrl = false, alt = false, shift = false}},
        {name = "x (no)",     ev = {type = "key", key = "x",   key_type = "runes", ctrl = false, alt = false, shift = false}},
        {name = "mouse (no)", ev = {type = "mouse", action = "press", button = "left"}},
    }
    for _, t in ipairs(tests) do
        local m = bindings.quit:matches(t.ev)
        local ind = m and theme.ok:render("[y]") or theme.fail:render("[n]")
        lines[#lines + 1] = "    " .. ind .. " " .. lbl:render(t.name)
    end

    lines[#lines + 1] = ""
    bindings.quit:set_enabled(false)
    local dm = bindings.quit:matches(tests[1].ev)
    bindings.quit:set_enabled(true)
    local em = bindings.quit:matches(tests[1].ev)

    lines[#lines + 1] = "  " .. sec:render("Enable/disable:")
    lines[#lines + 1] = "    " .. lbl:render("disabled: ") .. (dm and theme.ok or theme.fail):render(tostring(dm))
    lines[#lines + 1] = "    " .. lbl:render("enabled:  ") .. (em and theme.ok or theme.fail):render(tostring(em))
    lines[#lines + 1] = ""

    local nav_kb = tty.bind({
        keys = {"esc", "alt+x", "shift+tab", "ctrl+alt+delete"},
        help = {key = "esc", desc = "navigation"},
    })
    lines[#lines + 1] = "  " .. sec:render("Complex: ") .. val:render(tostring(nav_kb))

    local empty_kb = tty.bind({keys = {}})
    lines[#lines + 1] = "  " .. sec:render("Empty:   ") .. val:render(tostring(empty_kb))
    local nhelp = tty.bind({keys = {"a"}}):help()
    lines[#lines + 1] = "  " .. sec:render("No-help: ") .. "'" .. nhelp.key .. "'"
    lines[#lines + 1] = ""

    lines[#lines + 1] = "  " .. sec:render("Borders: ") ..
        val:render(NORMAL .. " " .. ROUNDED .. " " .. THICK .. " " .. DOUBLE .. " " .. HIDDEN)
    lines[#lines + 1] = "  " .. sec:render("Align:   ") ..
        val:render("L=" .. tty.align.LEFT .. " C=" .. tty.align.CENTER .. " R=" .. tty.align.RIGHT)
    lines[#lines + 1] = "  " .. sec:render("Pos:     ") ..
        val:render("T=" .. TOP .. " L=" .. LEFT .. " C=" .. CENTER .. " B=" .. tty.text.position.BOTTOM .. " R=" .. RIGHT)

    return tty.text.join_vertical(LEFT, title, "", table.concat(lines, "\n"))
end

-- Rendering

local tab_renderers = {render_events_tab, render_styles_tab, render_layout_tab, render_bindings_tab}

local function build_status_bar()
    local left = tty.style():foreground("#888888")
        :render(" " .. width .. "x" .. height
            .. " keys:" .. key_count
            .. " mouse:" .. mouse_count
            .. " resize:" .. resize_count
            .. " @" .. mouse_x .. "," .. mouse_y)

    local help_parts = {}
    for _, name in ipairs({"quit", "next_tab", "prev_tab"}) do
        local h = bindings[name]:help()
        help_parts[#help_parts + 1] = h.key .. ":" .. h.desc
    end
    local right = tty.style():foreground("#666666"):render(table.concat(help_parts, " ") .. " ")

    local gap = width - tty.text.width(left) - tty.text.width(right)
    if gap < 0 then gap = 0 end
    return theme.status:copy():width(width):render(left .. string.rep(" ", gap) .. right)
end

local function render()
    local header = theme.header:copy()
        :width(width):align(tty.align.CENTER):padding(0, 1)
        :render("TTY TOOLKIT DEMO")

    local tab_parts: {string} = {}
    for i, name in ipairs(tab_names) do
        local ts
        if i == tab_index then
            ts = tty.style():bold():foreground("#FFFFFF"):background("#0066FF"):padding(0, 1)
        else
            ts = tty.style():foreground("#888888"):padding(0, 1)
        end
        tab_parts[#tab_parts + 1] = ts:render(name)
    end
    local tab_bar = table.concat(tab_parts, "")
    tab_bar = tty.style():width(width):align(tty.align.CENTER):render(tab_bar)

    local renderer = tab_renderers[tab_index]
    local content: string = renderer and renderer(width, height - 4) or ""
    local status = build_status_bar()

    local frame = tty.text.join_vertical(LEFT, header, tab_bar, content)
    io.write(CURSOR_HOME .. frame .. ESC .. height .. ";1H" .. status .. CLEAR_BELOW)
    io.flush()
end

-- Event handling

local function handle_key(event: any)
    key_count = key_count + 1
    if bindings.quit:matches(event) then
        running = false
    elseif bindings.next_tab:matches(event) then
        tab_index = tab_index % #tab_names + 1
    elseif bindings.prev_tab:matches(event) then
        tab_index = (tab_index - 2) % #tab_names + 1
    end
end

local function handle_event(event: any)
    push_event(event)

    if event.type == "key" and event.action ~= "release" then
        handle_key(event)
    elseif event.type == "mouse" then
        mouse_count = mouse_count + 1
        mouse_x = event.x
        mouse_y = event.y
    elseif event.type == "resize" then
        resize_count = resize_count + 1
        width = event.width
        height = event.height
    elseif event.type == "start" then
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

    tty.mouse(true)
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
    tty.mouse(false)
    tty.stop()
end

return main
