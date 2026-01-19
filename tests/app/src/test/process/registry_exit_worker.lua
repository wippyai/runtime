-- Worker that registers a name then exits immediately
local function main()
    local inbox_ch = process.inbox()

    -- Wait for command
    local msg = inbox_ch:receive()
    if not msg or msg:topic() ~= "register_and_exit" then
        return false, "expected register_and_exit message"
    end

    local payload = msg:payload():data()
    local name = string(payload.name)

    -- Register self with the given name
    local ok, err = process.registry.register(name)
    if err then
        return false, "register failed: " .. tostring(err)
    end

    -- Exit immediately - name should be auto-released
    return "registered_and_exiting"
end

return { main = main }
