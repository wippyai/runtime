-- Workflow that receives signals via process.inbox()
local time = require("time")

local function main(input)
    local my_pid = process.pid()
    local timeout_ms = input and input.timeout_ms or 10000

    -- Wait for a message via process inbox
    local inbox = process.inbox()
    local msg, ok = inbox:receive(timeout_ms * time.MILLISECOND)

    if not ok then
        return {
            pid = my_pid,
            status = "timeout",
            error = "no message received within timeout"
        }
    end

    -- Get the actual payload data (msg:payload() returns a userdata wrapper)
    local payload_data = nil
    local p = msg:payload()
    if p then
        payload_data = p:data()
    end

    return {
        pid = my_pid,
        received_topic = msg:topic(),
        received_payload = payload_data,
        status = "received"
    }
end

return main
