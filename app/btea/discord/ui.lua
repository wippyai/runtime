local time = require("time")

local DiscordUI = {}
DiscordUI.__index = DiscordUI

-- Styles for the UI components
local styles = {
    box = btea.style()
         :border(btea.borders.ROUNDED)
         :padding(1, 2)
         :foreground("#89B4FA")
         :background("#1E1E2E")
         :border_foreground("#89B4FA")
         :border_top_foreground("#89B4FA")
         :border_bottom_foreground("#89B4FA")
         :border_left_foreground("#89B4FA")
         :border_right_foreground("#89B4FA"),

    header = btea.style()
        :bold()
        :foreground("#CBA6F7")
        :padding(0, 1),

    header_bar = btea.style()
        :foreground("#1E1E2E")
        :background("#CBA6F7")
        :bold()
        :padding(0, 1),

    header_clock = btea.style()
        :foreground("#1E1E2E")
        :background("#89B4FA")
        :bold()
        :padding(0, 1),

    header_status = btea.style()
        :foreground("#1E1E2E")
        :background("#F9E2AF")
        :bold()
        :padding(0, 1),

    status = btea.style()
        :foreground("#F9E2AF")
        :bold(),

    message = btea.style()
        :foreground("#89B4FA"),

    system = btea.style()
        :foreground("#F38BA8")
        :bold(),

    timestamp = btea.style()
        :foreground("#6C7086"),

    input_area = btea.style()
        :background("#313244")
        :padding(0, 1)
        :margin(1, 0)
}

function DiscordUI.new(window_size)
    local self = setmetatable({}, DiscordUI)
    self.window = window_size or { width = 80, height = 24 }
    self.messages = {}
    self.connection_status = "Starting..."
    self.active_channel = nil

    -- Initialize input
    self.input = btea.text_input({
        prompt = "> ",
        placeholder = "Type a message or command (!help, !channels, !listen)...",
        width = self.window.width - 2
    })

    -- Start clock ticker
    self:start_clock_ticker()

    return self
end

function DiscordUI:start_clock_ticker()
    self.current_time = time.now():format("15:04:05")
    coroutine.spawn(function()
        local ticker = time.ticker("1s")
        while true do
            local _, ok = ticker:channel():receive()
            if not ok then break end
            self.current_time = time.now():format("15:04:05")
        end
    end)
end

function DiscordUI:render_header()
    local bot_name = "Discord Bot"
    local status = self.connection_status
    local channel = self.active_channel and "#" .. self.active_channel.name or "None"
    local clock = self.current_time

    -- Calculate padding to fill the width
    local content_width = self.window.width - 18  -- Account for borders and padding
    local header_text = string.format("%s ⚫ %s ⚫ %s", bot_name, status, channel)
    local right_padding = content_width - #header_text - #clock

    -- Create the header bar with right-aligned clock
    return btea.text.join_horizontal(0,
        styles.header_bar:render(bot_name),
        styles.header_bar:render(" ⚫ "),
        styles.header_status:render(status),
        styles.header_bar:render(" ⚫ "),
        styles.header_bar:render(channel),
        styles.header_bar:render(string.rep(" ", right_padding)),
        styles.header_clock:render(clock)
    )
end

function DiscordUI:format_message(text, type)
    local timestamp = styles.timestamp:render(time.now():format("15:04:05"))
    if type == "system" then
        return timestamp .. " " .. styles.system:render(text)
    else
        return timestamp .. " " .. styles.message:render(text)
    end
end

function DiscordUI:add_message(text, type)
    table.insert(self.messages, self:format_message(text, type))
end

function DiscordUI:set_status(status)
    self.connection_status = status
end

function DiscordUI:set_active_channel(channel)
    self.active_channel = channel
end

function DiscordUI:set_window_size(size)
    self.window = size
    self.input:set_width(size.width - 10)
end

function DiscordUI:get_input_value()
    return self.input:value()
end

function DiscordUI:clear_input()
    self.input:set_value("")
end

function DiscordUI:render()
    local content_width = self.window.width - 6
    local header_divider = string.rep("─", content_width)

    local content = {
        self:render_header(),
        styles.timestamp:render(header_divider)
    }

    -- Calculate visible messages area
    local max_visible = self.window.height - 8  -- Adjusted for new header
    local start_idx = math.max(1, #self.messages - max_visible)

    for i = start_idx, #self.messages do
        table.insert(content, self.messages[i])
    end

    -- Add input field
    table.insert(content, "")
    table.insert(content, styles.input_area:render(self.input:view()))

    return styles.box
        :width(self.window.width - 2)
        :height(self.window.height - 2)
        :render(table.concat(content, "\n"))
end

function DiscordUI:update(msg)
    return self.input:update(msg)
end

function DiscordUI:focus()
    return self.input:focus()
end

return DiscordUI