local time = require("time")

-- Helper function to inspect table contents
local function inspect_table(t, depth)
    if type(t) ~= "table" then return tostring(t) end
    depth = depth or 0
    if depth > 5 then return "..." end -- Prevent infinite recursion

    local result = {}
    for k, v in pairs(t) do
        local val = type(v) == "table" and inspect_table(v, depth + 1) or tostring(v)
        table.insert(result, tostring(k) .. "=" .. val)
    end
    return "{" .. table.concat(result, ", ") .. "}"
end

local function run()
    -- Print our PID on start
    local my_pid = process.pid()
    print("Message listener process started with PID:", my_pid)

    -- Set up message listeners for both named topic and default inbox
    local msgs = process.listen("message")
    local inbox = process.inbox()
    local events = process.events()

    -- Main loop
    while true do
        local result = channel.select({
            msgs:case_receive(),
            inbox:case_receive(),
            events:case_receive()
        })

        if not result.ok then
            break
        end

        -- Handle single message from named topic
        if result.channel == msgs and result.value then
            local value = result.value
            print("Received message on 'message' topic:", inspect_table(value))

            -- Send response back to the function, if message contains from field
            if type(value) == "table" and value.from then
                process.send(value.from, "response", {
                    "Message received and processed on 'message' topic: " .. inspect_table(value.payload)
                })
            end
        end

        -- Handle message from default inbox
        if result.channel == inbox and result.value then
            local msg = result.value -- {topic="topic", payload={value}}
            print("Received message on default inbox topic '" .. tostring(msg.topic) .. "':",
                "payload=", inspect_table(msg.payload))

            -- Send response back if message contains from field
            process.send(msg.payload.from, "response", {
                "Message received and processed from inbox (original topic '" ..
                msg.topic .. "'): " .. inspect_table(msg.payload.payload)
            })
        end

        -- Handle events (like cancellation)
        if result.channel == events and result.value then
            local event = result.value
            if event.event.kind == process.EVENT_CANCEL then
                print("Process received cancel signal")
                break
            end
        end
    end

    print("Message listener process shutting down:", my_pid)
    return { status = "completed" }
end

return {
    run = run
}
