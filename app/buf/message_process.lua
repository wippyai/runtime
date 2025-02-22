local time = require("time")

local function run()
    -- Print our PID on start
    local my_pid = process.pid()
    print("Message listener process started with PID:", my_pid)

    -- Set up message listener
    local msgs = process.listen("message")
    local events = process.events()

    -- Main loop
    while true do
        local result = channel.select({
            msgs:case_receive(),
            events:case_receive()
        })

        if not result.ok then
            break
        end

        -- Handle messages
        if result.channel == msgs and result.value then
            local msg = result.value
            print("Received message:", msg.payload)

            -- Send response back to the function
            process.send(msg.from, "response", {
                "Message received and processed: " .. msg.payload
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