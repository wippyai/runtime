-- Service process that receives requests and spawns monitored workers
-- Reproduces the gov service pattern

local function main()
    local events_ch = process.events()
    local inbox_ch = process.inbox()

    -- Store pending operations by worker PID
    local pending = {}

    while true do
        local result = channel.select({
            inbox_ch:case_receive(),
            events_ch:case_receive()
        })

        if result.channel == inbox_ch then
            -- New request received via inbox (message object)
            local msg = result.value
            local from = msg:from()
            local payload = msg:payload():data()

            -- Spawn monitored worker
            local worker_pid, err = process.spawn_monitored(
                "app.test.process:spawn_monitored_worker",
                "app:processes",
                {
                    work_data = payload.work_data
                }
            )

            if err then
                -- Send error response immediately
                process.send(from, payload.respond_to, {
                    request_id = payload.request_id,
                    success = false,
                    error = tostring(err)
                })
            else
                -- Track pending operation
                pending[worker_pid] = {
                    from = from,
                    respond_to = payload.respond_to,
                    request_id = payload.request_id
                }
            end

        elseif result.channel == events_ch then
            -- Event received
            local event = result.value

            if event.kind == process.event.EXIT then
                -- Worker finished
                local operation = pending[event.from]
                if operation then
                    pending[event.from] = nil

                    local response = {
                        request_id = operation.request_id,
                        success = event.result.error == nil,
                        result = event.result.value
                    }

                    process.send(operation.from, operation.respond_to, response)
                end
            elseif event.kind == process.event.CANCEL then
                -- Service cancelled, exit
                return true
            end
        end
    end
    return true
end

return { main = main }
