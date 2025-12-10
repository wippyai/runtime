-- Child that waits for LINK_DOWN event
local function main()
    local events_ch = process.events()

    -- Wait for event (blocking)
    local event = events_ch:receive()

    return "received:" .. tostring(event.kind)
end

return { main = main }
