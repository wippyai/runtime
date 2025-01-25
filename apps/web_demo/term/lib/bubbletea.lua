-- bubbletea.lua - Terminal UI Components Library
local M = {}

-- ANSI color codes and styles
M.style = {
    -- Colors
    colors = {
        black = "\27[30m",
        red = "\27[31m",
        green = "\27[32m",
        yellow = "\27[33m",
        blue = "\27[34m",
        magenta = "\27[35m",
        cyan = "\27[36m",
        white = "\27[37m",
        reset = "\27[0m",
        -- Bright variants
        bright_black = "\27[90m",
        bright_red = "\27[91m",
        bright_green = "\27[92m",
        bright_yellow = "\27[93m",
        bright_blue = "\27[94m",
        bright_magenta = "\27[95m",
        bright_cyan = "\27[96m",
        bright_white = "\27[97m",
    },

    -- Text effects
    bold = "\27[1m",
    dim = "\27[2m",
    italic = "\27[3m",
    underline = "\27[4m",
    reverse = "\27[7m",
    hidden = "\27[8m",
    strike = "\27[9m",

    -- Reset specific attributes
    reset_bold = "\27[22m",
    reset_dim = "\27[22m",
    reset_italic = "\27[23m",
    reset_underline = "\27[24m",
    reset_reverse = "\27[27m",
    reset_hidden = "\27[28m",
    reset_strike = "\27[29m",
}

-- String manipulation utilities
M.str = {
    -- Calculate visual length of string (ignoring ANSI codes)
    len = function(str)
        return #(str:gsub("\27%[[%d;]*m", ""))
    end,

    -- Pad string to specified length
    pad = function(str, width, char)
        char = char or " "
        local len = M.str.len(str)
        if len >= width then return str end
        return str .. string.rep(char, width - len)
    end,

    -- Center string in specified width
    center = function(str, width, char)
        char = char or " "
        local len = M.str.len(str)
        if len >= width then return str end
        local left = math.floor((width - len) / 2)
        local right = width - len - left
        return string.rep(char, left) .. str .. string.rep(char, right)
    end,
}

-- Box drawing components
M.box = {
    chars = {
        h = "─",
        v = "│",
        tl = "┌",
        tr = "┐",
        bl = "└",
        br = "┘",
        vr = "├",
        vl = "┤",
        hb = "┬",
        ht = "┴",
        c = "┼",
    },

    -- Create border line
    border = function(width, left, mid, right)
        return left .. string.rep(mid, width - 2) .. right
    end,

    -- Create complete box
    create = function(width, height, content)
        local ch = M.box.chars
        local lines = {
            M.box.border(width, ch.tl, ch.h, ch.tr)
        }

        for i = 1, height - 2 do
            local content_line = content and content[i] or ""
            table.insert(lines, ch.v .. M.str.pad(content_line, width - 2) .. ch.v)
        end

        table.insert(lines, M.box.border(width, ch.bl, ch.h, ch.br))
        return table.concat(lines, "\n")
    end,
}

-- Common UI components
M.components = {
    -- Progress bar
    progress = function(width, value, opts)
        opts = opts or {}
        local fill = opts.fill or "█"
        local empty = opts.empty or "░"
        local color = opts.color or M.style.colors.blue
        local reset = M.style.colors.reset

        local filled = math.floor(value * (width - 2))
        local empty_space = width - 2 - filled

        return M.box.chars.v ..
            color .. string.rep(fill, filled) ..
            string.rep(empty, empty_space) .. reset ..
            M.box.chars.v
    end,

    -- Spinner component with multiple styles
    spinner = function(style)
        local frames
        if style == "dots" then
            frames = { "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏" }
        elseif style == "line" then
            frames = { "-", "\\", "|", "/" }
        else
            frames = { "◜", "◠", "◝", "◞", "◡", "◟" }
        end

        local current = 0
        return {
            next = function()
                current = (current % #frames) + 1
                return frames[current]
            end
        }
    end,
}

-- Layout management
M.layout = {
    -- Stack elements horizontally
    h_stack = function(elements, spacing)
        spacing = spacing or 1
        local max_height = 0
        local elem_lines = {}

        -- Split all elements into lines and find max height
        for _, elem in ipairs(elements) do
            local lines = {}
            for line in elem:gmatch("[^\n]+") do
                table.insert(lines, line)
            end
            table.insert(elem_lines, lines)
            max_height = math.max(max_height, #lines)
        end

        -- Combine lines
        local result = {}
        for i = 1, max_height do
            local line = {}
            for j, elem in ipairs(elem_lines) do
                table.insert(line, elem[i] or string.rep(" ", M.str.len(elem[1] or "")))
                if j < #elem_lines then
                    table.insert(line, string.rep(" ", spacing))
                end
            end
            table.insert(result, table.concat(line))
        end

        return table.concat(result, "\n")
    end,

    -- Stack elements vertically
    v_stack = function(elements, spacing)
        spacing = spacing or 1
        return table.concat(elements, string.rep("\n", spacing))
    end,
}

-- Message handling helpers
M.msg = {
    -- Parse window size from message
    parse_size = function(msg)
        if type(msg) ~= "table" or msg.type ~= "update" or not msg.msg then
            return nil
        end

        local width, height = msg.msg:match("{(%d+) (%d+)}")
        if width and height then
            return {
                width = tonumber(width),
                height = tonumber(height)
            }
        end
        return nil
    end,

    -- Parse key event from message
    parse_key = function(msg)
        if type(msg) ~= "table" or msg.type ~= "update" or not msg.key then
            return nil
        end

        return {
            key = msg.key.String,
            alt = msg.key.Alt,
            type = msg.key.Type
        }
    end,
}

return M
