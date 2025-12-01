-- Simple worker process that receives messages via inbox

local function main(input)
    -- Get our PID
    local pid = process.pid()
    print("[WORKER] Started with PID: " .. pid)

    -- Process input if provided
    if input then
        print("[WORKER] Received input: " .. tostring(input))
    end

    -- Get inbox channel for receiving messages
    local inbox = process.inbox()
    print("[WORKER] Waiting for messages on inbox...")

    -- Wait for a message (blocking receive)
    local msg, ok = inbox:receive()

    if ok and msg then
        print("[WORKER] Got message!")
        print("[WORKER]   Topic: " .. tostring(msg:topic()))
        print("[WORKER]   From: " .. tostring(msg:from()))
    else
        print("[WORKER] Inbox closed or no message")
    end

    print("[WORKER] Completed")
    return "done"
end

return {
    main = main
}
