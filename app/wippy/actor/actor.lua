local actor = {}

local function is_exit(result)
    return type(result) == "table" and result._actor_exit == true
end

function actor.exit(result)
    return {
        _actor_exit = true,
        result = result
    }
end

function actor.new(initial_state, handlers)
    if type(handlers) ~= "table" then
        error("handlers must be a table")
    end

    local function run_loop(state)
        local inbox = process.inbox()
        local events = process.events()
        -- Listen on dedicated message topic if handler exists
        local msgs = handlers.message and process.listen("message") or nil

        local select_cases = {
            inbox:case_receive(),
            events:case_receive()
        }
        if msgs then
            table.insert(select_cases, msgs:case_receive())
        end

        while true do
            local result = channel.select(select_cases)

            if not result.ok then
                break
            end

            -- Handle cancellation
            if result.channel == events and result.value then
                local event = result.value
                if event.event.kind == process.event.CANCEL and handlers.on_cancel then
                    local exit_result = handlers.on_cancel(state)
                    if is_exit(exit_result) then
                        return exit_result.result
                    end
                end
            end

            -- Handle dedicated message topic
            if msgs and result.channel == msgs and result.value then
                local value = result.value
                local reply = handlers.message(state, value)
                if is_exit(reply) then
                    return reply.result
                end
            end

            -- Handle inbox messages
            if result.channel == inbox and result.value then
                local msg = result.value
                local handler = handlers[msg.topic]

                if handler then
                    local reply = handler(state, msg.payload)
                    if is_exit(reply) then
                        return reply.result
                    end
                    if reply and msg.payload.reply_to then
                        process.send(msg.payload.reply_to, msg.topic .. "_reply", reply)
                    end
                elseif handlers.__default then
                    -- Call default handler with topic as extra param
                    local reply = handlers.__default(state, msg.payload, msg.topic)
                    if is_exit(reply) then
                        return reply.result
                    end
                end
            end
        end

        return { status = "completed" }
    end

    return {
        run = function() return run_loop(initial_state) end
    }
end

return actor
