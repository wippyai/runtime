-- Pure update function that handles input message
local function handle_update(msg)
    if msg.tick then
        return "got tick"
    elseif msg.key then
        return "got key: " .. msg.key.String
    end
    return "unknown message"
end

-- Pure view function that returns current view
local function handle_view(msg)
    return [[
╔════════════════════════╗
║    Simple App View     ║
║                        ║
║  Press 'q' to quit     ║
╚════════════════════════╝
]]
end

function App()
    local inbox = tasks.channel()

    -- Start background coroutine
    coroutine.spawn(function()
        -- Create a ticker for every second
        local ticker = time.ticker("1s")
        while true do
            -- Wait for tick
            ticker:channel():receive()
            -- Send message upstream
            upstream.send("tick")
        end
    end)

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            break
        end

        local msg = task:input()
        if msg.type == "update" then
            -- Call pure update function and complete task with result
            task:complete(handle_update(msg))
        elseif msg.type == "view" then
            -- Call pure view function and complete task with result
            task:complete(handle_view(msg))
        else
            -- Complete unknown tasks with ok
            task:complete("ok")
        end
    end
end

return App
