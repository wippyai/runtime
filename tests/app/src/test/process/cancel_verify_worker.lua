-- Worker that waits for CANCEL event and exits
local function main()
    local events_ch = process.events()

    -- Wait for CANCEL event (blocking)
    local event = events_ch:receive()

    if event.kind ~= process.event.CANCEL then
        return false, "expected CANCEL, got: " .. tostring(event.kind)
    end

    return "cancelled"
end

return { main = main }
