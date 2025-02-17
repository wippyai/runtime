local function run()
    local events_ch = pubsub.subscribe("@pid/events")
    local done = channel.new()
    local is_running = true

    while is_running do
        local result = channel.select({
            events_ch:case_receive(),
            done:case_receive()
        })

        if not result.ok then
            break
        end

        if result.channel == done then
            break
        end

        local event = result.value
        if not event then
            break
        end

        -- Handle shutdown event
        if event.type == "shutdown" then
            is_running = false
            break
        end
    end

    done:close()
end

return {
    run = run
}