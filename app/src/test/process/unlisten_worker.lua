-- Worker: Listen, receive one message, unlisten, verify no more
local time = require("time")

local function main()
    local inbox_ch = process.inbox()
    local topic_ch = process.listen("test_topic")

    local received_count = 0

    -- Wait for first message on topic
    local timeout = time.after("2s")
    local result = channel.select {
        topic_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= topic_ch then
        error("timeout waiting for first message")
    end
    received_count = received_count + 1

    -- Unlisten from topic
    process.unlisten(topic_ch)

    -- Wait for done signal on inbox (second message on test_topic should be dropped)
    timeout = time.after("2s")
    result = channel.select {
        inbox_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= inbox_ch then
        error("timeout waiting for done signal")
    end

    return true
end

return { main = main }
