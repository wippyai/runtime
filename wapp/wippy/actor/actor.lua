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

        -- Setup for topic-specific handlers from the handlers table
        local topic_handlers = {}

        -- Find handlers that should be associated with specific topics
        for name, handler in pairs(handlers) do
            -- Special handlers start with __, others are assumed to be topic handlers
            if type(handler) == "function" and not name:match("^__") then
                topic_handlers[name] = handler
            end
        end

        -- Registered channels and their handlers
        local registered_channels = {}
        -- Map channel objects to their string IDs for quick lookups
        local channel_to_id = {}

        -- Build select cases starting with core channels
        local select_cases = {
            inbox:case_receive(),
            events:case_receive()
        }

        -- Function to rebuild select cases when channels change
        local function rebuild_select_cases()
            select_cases = {
                inbox:case_receive(),
                events:case_receive()
            }

            for channel_id, channel_info in pairs(registered_channels) do
                local rec = channel_info.chan:case_receive()
                table.insert(select_cases, rec)
            end
        end

        -- Function to register a new channel and its handler
        local function register_channel(chan, handler)
            if not chan or type(handler) ~= "function" then
                error("Channel and handler function must be provided")
            end

            local channel_id = tostring(chan)
            registered_channels[channel_id] = { chan = chan, handler = handler }
            channel_to_id[chan] = channel_id

            -- Rebuild select cases with the new channel
            rebuild_select_cases()

            return true
        end

        -- Function to unregister a channel
        local function unregister_channel(chan)
            local channel_id = tostring(chan)
            if registered_channels[channel_id] then
                registered_channels[channel_id] = nil
                channel_to_id[chan] = nil
                rebuild_select_cases()
                return true
            end
            return false
        end

        -- Function to add a message topic handler
        local function add_handler(topic, handler)
            if not topic or type(handler) ~= "function" then
                error("Topic name and handler function must be provided")
            end

            topic_handlers[topic] = handler
            return true
        end

        -- Function to remove a message topic handler
        local function remove_handler(topic)
            if topic_handlers[topic] then
                topic_handlers[topic] = nil
                return true
            end
            return false
        end

        -- Add management functions to state
        state.register_channel = register_channel
        state.unregister_channel = unregister_channel
        state.add_handler = add_handler
        state.remove_handler = remove_handler

        -- Call __init handler if it exists
        if handlers.__init then
            local init_result = handlers.__init(state)
            if is_exit(init_result) then
                return init_result.result
            end
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

                local handler = topic_handlers[topic]

                if handler then
                    local reply = handler(state, payload)
                    if is_exit(reply) then
                        return reply.result
                    end
                elseif handlers.__default then
                    -- Call default handler with topic as extra param
                    local reply = handlers.__default(state, topic, payload)
                    if is_exit(reply) then
                        return reply.result
                    end
                end
            end

            -- Check for channel handlers using the direct channel object lookup
            local channel_id = channel_to_id[result.channel]
            if channel_id then
                local channel_info = registered_channels[channel_id]
                local handler = channel_info.handler

                -- Call handler with the received value (or nil if channel closed)
                -- Third parameter indicates if channel is still open (ok value)
                local reply = handler(state, result.value, result.ok)

                -- If channel was closed, automatically unregister it
                if not result.ok then
                    registered_channels[channel_id] = nil
                    channel_to_id[result.channel] = nil
                    rebuild_select_cases()
                end

                if is_exit(reply) then
                    return reply.result
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
