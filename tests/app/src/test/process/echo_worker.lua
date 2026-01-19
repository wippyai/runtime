-- Worker: Echo messages back to sender
local function main()
    -- Subscribe to "echo" topic with message mode to get :from()
    local ch = process.listen("echo", { message = true })

    -- Wait for one message and echo it back
    local msg = ch:receive()
    if msg then
        local sender = string(msg:from())
        local payload = msg:payload()

        if sender then
            -- Send the payload back to the sender
            process.send(sender, "reply", payload)
        end
        return true
    end

    return false
end

return { main = main }
