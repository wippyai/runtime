-- Worker: Sends message to custom topic then exits
local function main(parent_pid, topic)
    if not parent_pid then
        error("parent_pid required")
    end
    if not topic then
        error("topic required")
    end

    local ok, err = process.send(parent_pid, topic, "custom_message")
    if err then
        error("send failed: " .. tostring(err))
    end

    return true
end

return { main = main }
