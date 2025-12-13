-- Child that waits for LINK_DOWN event
local function main()
    -- Enable trap_links to receive LINK_DOWN events
    local ok, err = process.set_options({ trap_links = true })
    if not ok then
        return false, "set_options failed: " .. tostring(err)
    end

    local events_ch = process.events()

    -- Wait for event (blocking)
    local event = events_ch:receive()

    return "received:" .. tostring(event.kind)
end

return { main = main }
