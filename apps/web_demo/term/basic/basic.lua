-- Import the bubbletea library
local tea = require("bubbletea.lib")

-- Operation log to store all operations
local operations = {}
local window_width = 80  -- default width
local window_height = 24 -- default height

-- Helper function to format key events more readably
local function format_key_event(key)
    if not key then return "nil" end
    local colors = tea.style.colors
    return string.format("Key '%s%s%s' (Alt: %s)",
        colors.cyan, key.String, colors.reset,
        key.Alt and colors.yellow.."yes"..colors.reset or colors.red.."no"..colors.reset)
end

-- Log operation with timestamp using time module
local function log_operation(op_type, msg)
    local now = time.now()
    local timestamp = now:format("15:04:05")
    local colors = tea.style.colors

    -- Format the message based on type
    local formatted
    if type(msg) == "string" then
        formatted = colors.green .. msg .. colors.reset
    else
        -- Check for special message types
        local size = tea.msg.parse_size(msg)
        if size then
            window_width = size.width
            window_height = size.height
            formatted = colors.yellow.."UPDATE: "..colors.reset.."window size: "..size.width.."x"..size.height
        else
            local key = tea.msg.parse_key(msg)
            if key then
                formatted = colors.yellow.."UPDATE: "..colors.reset..format_key_event(msg.key)
            elseif msg.type == "update" and msg.msg == "tick" then
                formatted = colors.yellow.."UPDATE: "..colors.reset..colors.magenta.."tick event"..colors.reset
            elseif msg.type == "view" then
                formatted = colors.blue.."VIEW: refresh request"..colors.reset
            else
                formatted = tea.str.dump_value(msg)
            end
        end
    end

    -- Skip logging VIEW refreshes that immediately follow updates
    if op_type == "VIEW" and msg.type == "view" and #operations > 0 then
        local last_op = operations[#operations]
        if last_op:match("^%[%d%d:%d%d:%d%d%] UPDATE:") then
            return
        end
    end

    -- Color the operation type
    local color = colors.white
    if op_type == "UPDATE" then color = colors.yellow
    elseif op_type == "VIEW" then color = colors.blue
    elseif op_type == "TICKER" then color = colors.magenta
    elseif op_type == "MAIN" then color = colors.red
    end

    table.insert(operations, string.format("[%s%s%s] %s%s%s: %s",
        colors.green, timestamp, colors.reset,
        color, op_type, colors.reset,
        formatted))

    -- Keep last N operations based on window height
    local max_logs = window_height - 8  -- Save space for borders and headers
    if max_logs < 5 then max_logs = 5 end -- Minimum 5 logs
    while #operations > max_logs do
        table.remove(operations, 1)
    end
end

-- Pure update function that handles input message
local function handle_update(msg)
    log_operation("UPDATE", msg)
    if msg.tick then
        return "got tick"
    elseif msg.key then
        return "got key: " .. msg.key.String
    end
    return "unknown message"
end

-- Create view layout sections
local function create_header(content_width)
    local colors = tea.style.colors
    local title = colors.cyan.."Simple App View"..colors.reset
    local quit_msg = colors.yellow.."Press 'q' to quit"..colors.reset

    return tea.layout.v_stack({
        tea.str.center(title, content_width),
        tea.str.pad(" "..quit_msg, content_width)
    }, 0)
end

local function create_log_section(content_width)
    local colors = tea.style.colors
    local log_lines = {
        tea.str.pad(" "..colors.magenta.."Operation Log:"..colors.reset, content_width)
    }

    for _, op in ipairs(operations) do
        table.insert(log_lines, tea.str.pad(" "..op, content_width))
    end

    return tea.layout.v_stack(log_lines, 0)
end

-- Pure view function that returns current view
local function handle_view(msg)
    log_operation("VIEW", msg)

    -- Calculate usable width (subtracting border chars)
    local content_width = window_width - 2

    -- Create view sections
    local header = create_header(content_width)
    local log_section = create_log_section(content_width)

    -- Combine sections into final view
    return tea.box.create(window_width, window_height, {
        header,
        "",  -- Spacer before log section
        log_section
    })
end

function App()
    local inbox = tasks.channel()
    -- Create a done channel to signal shutdown
    local done = channel.new()

    -- Start background coroutine
    coroutine.spawn(function()
        -- Create a ticker for every second
        local ticker = time.ticker("1s")
        while true do
            -- Use select to either receive tick or done signal
            local result = channel.select{
                ticker:channel():case_receive(),
                done:case_receive()
            }

            -- If we got done signal, break the loop
            if result.channel == done then
                break
            end

            -- Otherwise send tick message upstream
            log_operation("TICKER", "sending tick")
            upstream.send("tick")
        end
    end)

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            -- Signal background coroutine to stop before breaking
            log_operation("MAIN", "inbox closed, sending done signal")
            done:send(true)
            break
        end

        local msg = task:input()
        if msg.type == "update" then
            -- Call pure update function and complete task with result
            local result = handle_update(msg)
            task:complete(result)
        elseif msg.type == "view" then
            -- Call pure view function and complete task with result
            local result = handle_view(msg)
            task:complete(result)
        else
            -- Complete unknown tasks with ok
            log_operation("UNKNOWN", msg)
            task:complete("ok")
        end
    end

    -- Ensure the done channel is closed
    done:close()
    log_operation("MAIN", "done channel closed")
end

return App