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

-- Default implementation that uses the global process object
local default_process = {
    inbox = function() return process.inbox() end,
    events = function() return process.events() end,
    listen = function(topic) return process.listen(topic) end,
    send = function(dest, topic, payload) return process.send(dest, topic, payload) end,
    pid = function() return process.pid() end,
    event = process.event
}

function actor.new(initial_state, handlers, proc)
    if type(handlers) ~= "table" then
        error("handlers must be a table")
    end

    -- Use provided process implementation or default to global process
    local proc_impl = proc or default_process

    local function run_loop(state)
        local inbox = proc_impl.inbox()
        local events = proc_impl.events()

        -- Set up topic-specific listeners
        local topic_listeners = {}
        local topic_channels = {}

        -- Find handlers that should be associated with specific topics
        for name, handler in pairs(handlers) do
            -- Special handlers start with __, others are assumed to be topic handlers
            if type(handler) == "function" and not name:match("^__") then
                topic_listeners[name] = handler
                topic_channels[name] = proc_impl.listen(name)
            end
        end

        -- Build select cases starting with core channels
        local select_cases = {
            inbox:case_receive(),
            events:case_receive()
        }

        -- Add topic-specific channels to select cases
        for topic, channel in pairs(topic_channels) do
            table.insert(select_cases, channel:case_receive())
        end

        while true do
            local result = channel.select(select_cases)
            if not result.ok then
                break
            end

            -- Handle system events
            if result.channel == events and result.value then
                local event = result.value

                -- Call __on_event handler for all system events if it exists
                if handlers.__on_event then
                    local exit_result = handlers.__on_event(state, event)
                    if is_exit(exit_result) then
                        return exit_result.result
                    end
                end

                -- Special handling for cancellation events
                if event.kind == proc_impl.event.CANCEL and handlers.__on_cancel then
                    local exit_result = handlers.__on_cancel(state)
                    if is_exit(exit_result) then
                        return exit_result.result
                    end
                end
            end

            -- Handle inbox messages
            if result.channel == inbox and result.value then
                -- inbox allows us to access raw values
                local msg = result.value

                -- Extract topic and payload from message
                local topic = msg:topic()
                local payload_ud = msg:payload()
                local payload = payload_ud:data()

                local handler = handlers[topic]

                if handler then
                    local reply = handler(state, payload)
                    if is_exit(reply) then
                        return reply.result
                    end
                    if reply and payload.reply_to then
                        proc_impl.send(payload.reply_to, topic .. "_reply", reply)
                    end
                elseif handlers.__default then
                    -- Call default handler with topic as extra param
                    local reply = handlers.__default(state, payload, topic)
                    if is_exit(reply) then
                        return reply.result
                    end
                end
            end

            -- Check if this was a topic-specific channel
            for topic, channel in pairs(topic_channels) do
                if result.channel == channel and result.value then
                    local message = result.value
                    local handler = topic_listeners[topic]

                    if handler then
                        local reply = handler(state, message)
                        if is_exit(reply) then
                            return reply.result
                        end
                    end

                    break -- Found the channel, no need to check others
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
