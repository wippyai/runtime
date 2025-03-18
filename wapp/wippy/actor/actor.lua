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

    local proc_impl = proc or default_process

    local function run_loop(state)
        local inbox = proc_impl.inbox()
        local events = proc_impl.events()
        local internal_channel = channel.new(100)

        local topic_handlers = {}
        for name, handler in pairs(handlers) do
            if type(handler) == "function" and not name:match("^__") then
                topic_handlers[name] = handler
            end
        end

        local registered_channels = {}
        local channel_to_id = {}
        local async_tasks = {}

        local select_cases = {
            inbox:case_receive(),
            events:case_receive(),
            internal_channel:case_receive()
        }

        local function rebuild_select_cases()
            select_cases = {
                inbox:case_receive(),
                events:case_receive(),
                internal_channel:case_receive()
            }

            for channel_id, channel_info in pairs(registered_channels) do
                table.insert(select_cases, channel_info.chan:case_receive())
            end
        end

        local function register_channel(chan, handler)
            if not chan or type(handler) ~= "function" then
                error("Channel and handler function must be provided")
            end

            local channel_id = tostring(chan)
            registered_channels[channel_id] = { chan = chan, handler = handler }
            channel_to_id[chan] = channel_id
            rebuild_select_cases()
            return true
        end

        local function unregister_channel(chan)
            if not chan then return false end

            local channel_id = tostring(chan)
            if registered_channels[channel_id] then
                registered_channels[channel_id] = nil
                channel_to_id[chan] = nil
                rebuild_select_cases()
                return true
            end
            return false
        end

        local function add_handler(topic, handler)
            if not topic or type(handler) ~= "function" then
                error("Topic name and handler function must be provided")
            end
            topic_handlers[topic] = handler
            return true
        end

        local function remove_handler(topic)
            if topic_handlers[topic] then
                topic_handlers[topic] = nil
                return true
            end
            return false
        end

        local function post(msg_type, payload)
            internal_channel:send({ type = msg_type, payload = payload })
            return true
        end

        local function async(fn)
            local async_task = {
                id = tostring({}),
                _then = nil
            }

            function async_task.on_complete(callback)
                async_task._then = callback
                return async_task
            end

            async_tasks[async_task.id] = async_task

            coroutine.spawn(function()
                local result, err = fn()
                internal_channel:send({
                    type = "async_result",
                    task_id = async_task.id,
                    result = result,
                    error = err
                })
            end)

            return async_task
        end

        state.register_channel = register_channel
        state.unregister_channel = unregister_channel
        state.add_handler = add_handler
        state.remove_handler = remove_handler
        state.post = post
        state.async = async

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

            if result.channel == events and result.value then
                local event = result.value

                if handlers.__on_event then
                    local exit_result = handlers.__on_event(state, event)
                    if is_exit(exit_result) then
                        return exit_result.result
                    end
                end

                if event.kind == proc_impl.event.CANCEL and handlers.__on_cancel then
                    local exit_result = handlers.__on_cancel(state)
                    if is_exit(exit_result) then
                        return exit_result.result
                    end
                end
            end

            if result.channel == inbox and result.value then
                local msg = result.value
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
                    local reply = handlers.__default(state, payload, topic)
                    if is_exit(reply) then
                        return reply.result
                    end
                end
            end

            if result.channel == internal_channel and result.value then
                local msg = result.value

                if msg.type == "async_result" and async_tasks[msg.task_id] then
                    local task = async_tasks[msg.task_id]
                    if task._then then
                        local reply = task._then(state, msg.result, msg.error)
                        if is_exit(reply) then
                            return reply.result
                        end
                    end
                    async_tasks[msg.task_id] = nil
                elseif handlers.__on_internal_message then
                    local reply = handlers.__on_internal_message(state, msg.type, msg.payload)
                    if is_exit(reply) then
                        return reply.result
                    end
                end
            end

            local channel_id = channel_to_id[result.channel]
            if channel_id then
                local channel_info = registered_channels[channel_id]
                local reply = channel_info.handler(state, result.value, result.ok)

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