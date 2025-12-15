-- Worker that registers itself with a name and echoes messages
local time = require("time")

local function main()
    local inbox_ch = process.inbox()
    local events_ch = process.events()

    -- Wait for registration command
    local msg = inbox_ch:receive()
    if not msg or msg:topic() ~= "register" then
        return false, "expected register message"
    end

    local payload = msg:payload():data()
    local name = payload.name
    local reply_to = payload.reply_to

    -- Register self with the given name
    local ok, err = process.registry.register(name)
    if err then
        return false, "register failed: " .. tostring(err)
    end

    -- Confirm registration
    process.send(reply_to, "registered", { name = name })

    -- Echo loop - handle messages and cancel
    while true do
        local result = channel.select {
            inbox_ch:case_receive(),
            events_ch:case_receive(),
        }

        if result.channel == events_ch then
            local event = result.value
            if event.kind == process.event.CANCEL then
                return "cancelled"
            end
        elseif result.channel == inbox_ch then
            msg = result.value
            if not msg then
                break
            end

            local topic = msg:topic()
            if topic == "echo" then
                local data = msg:payload():data()
                process.send(data.reply_to, "echo_reply", { echoed = data.value })
            end
        end
    end

    return true
end

return { main = main }
