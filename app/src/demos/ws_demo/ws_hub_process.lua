local time = require("time")
local json = require("json")

-- Blob Game WebSocket Hub Process
local function run()
    -- Initialize game state
    local state = {
        -- Client connection tracking
        connected_clients = {}, -- Map of client_pid -> { force = {x=0, y=0} }
        client_count = 0,

        -- Blob physics
        blob = {
            position = { x = 400, y = 300 }, -- Starting position (will be updated to canvas center)
            velocity = { x = 0, y = 0 },     -- Current velocity
            radius = 40,                     -- Blob radius
            mass = 0.0005,                   -- Mass affects how quickly it responds to forces
            friction = 0.985,                -- Friction coefficient (0-1), lower = more friction
            boundary = {                     -- Boundaries to keep blob in canvas
                x_min = 50, x_max = 750,     -- Will be updated based on client dimensions
                y_min = 50, y_max = 550
            }
        },

        -- Game settings
        total_updates = 0
    }

    -- Register this process with a name for easy discovery
    process.registry.register("ws_hub")
    print("Blob Game Hub started with PID:", process.pid())

    -- Create channels for events
    local inbox = process.inbox()        -- For getting messages from clients
    local ticker = time.ticker("20ms")   -- For physics & broadcast updates
    local events = process.events()      -- For process lifecycle events

    -- Set process options
    process.set_options({
        trap_links = true  -- We want to handle link downs
    })

    -- Helper function to safely extract sender information
    local function get_sender_info(msg)
        local sender = "unknown"
        if msg and type(msg.from) == "function" then
            local from_result = msg:from()
            if from_result then
                sender = from_result
            end
        end
        return sender
    end

    -- Helper function to extract client_pid from payload
    local function extract_client_pid(payload)
        -- Handle different payload types
        if type(payload) == "string" then
            return payload -- Direct string PID
        elseif type(payload) == "table" then
            -- Check for structured payload
            if payload.client_pid then
                return payload.client_pid
            elseif payload.ws_pid then
                return payload.ws_pid
            end
        end
        return nil
    end

    -- Physics: Update blob position based on all forces
    local function update_physics()
        -- Reset accumulated force
        local total_force = { x = 0, y = 0 }

        -- Accumulate forces from all clients
        for _, client in pairs(state.connected_clients) do
            total_force.x = total_force.x + client.force.x
            total_force.y = total_force.y + client.force.y
        end

        -- Fixed time step for consistent physics
        local dt = 0.02 -- 20ms in seconds

        -- Apply force to velocity (F = ma, so a = F/m)
        state.blob.velocity.x = state.blob.velocity.x + (total_force.x / state.blob.mass) * dt
        state.blob.velocity.y = state.blob.velocity.y + (total_force.y / state.blob.mass) * dt

        -- Apply friction to velocity
        state.blob.velocity.x = state.blob.velocity.x * state.blob.friction
        state.blob.velocity.y = state.blob.velocity.y * state.blob.friction

        -- Update position based on velocity
        state.blob.position.x = state.blob.position.x + state.blob.velocity.x * dt
        state.blob.position.y = state.blob.position.y + state.blob.velocity.y * dt

        -- Enforce boundaries with bouncing
        if state.blob.position.x < state.blob.boundary.x_min then
            state.blob.position.x = state.blob.boundary.x_min
            state.blob.velocity.x = -state.blob.velocity.x * 0.5  -- Bounce with energy loss
        elseif state.blob.position.x > state.blob.boundary.x_max then
            state.blob.position.x = state.blob.boundary.x_max
            state.blob.velocity.x = -state.blob.velocity.x * 0.5
        end

        if state.blob.position.y < state.blob.boundary.y_min then
            state.blob.position.y = state.blob.boundary.y_min
            state.blob.velocity.y = -state.blob.velocity.y * 0.5
        elseif state.blob.position.y > state.blob.boundary.y_max then
            state.blob.position.y = state.blob.boundary.y_max
            state.blob.velocity.y = -state.blob.velocity.y * 0.5
        end
    end

    -- Broadcast current blob position to all clients
    local function broadcast_position()
        -- Skip if no clients connected
        if state.client_count == 0 then
            return
        end

        state.total_updates = state.total_updates + 1

        -- Create message with current blob state
        local update = {
            type = "update",
            time = time.now():format_rfc3339(),
            sequence = state.total_updates,
            clients = state.client_count,
            position = state.blob.position,
            velocity = state.blob.velocity
        }

        -- Only log occasionally to reduce spam
        if state.total_updates % 50 == 0 then
            print(string.format("Broadcasting position update #%d to %d clients: (%0.2f, %0.2f)",
                state.total_updates, state.client_count,
                state.blob.position.x, state.blob.position.y))
        end

        -- Broadcast to all clients
        for client_pid, _ in pairs(state.connected_clients) do
            if client_pid then
                process.send(client_pid, "ws.message", update)
            end
        end
    end

    -- Process force update from a client
    local function process_force_update(client_pid, force_data)
        -- Ignore invalid messages
        if not client_pid or not force_data or type(force_data) ~= "table" then
            return
        end

        -- Extract force values
        local force_x = tonumber(force_data.x) or 0
        local force_y = tonumber(force_data.y) or 0

        -- Cap maximum force (prevent cheating/exploits)
        local max_force = 10
        force_x = math.max(-max_force, math.min(max_force, force_x))
        force_y = math.max(-max_force, math.min(max_force, force_y))

        -- Update client force
        if state.connected_clients[client_pid] then
            state.connected_clients[client_pid].force = { x = force_x, y = force_y }
        end
    end

    -- Main event loop
    local running = true
    while running do
        local result = channel.select({
            inbox:case_receive(),
            ticker:channel():case_receive(),
            events:case_receive()
        })

        if result.channel == inbox then
            -- Process incoming message from relay
            local msg = result.value
            if not msg then
                goto continue
            end

            local topic = msg:topic()
            if not topic then
                goto continue
            end

            -- Safely extract payload
            local payload = nil
            if msg:payload() then
                payload = msg:payload():data()
            end

            -- If payload is nil, skip processing
            if payload == nil then
                print("Warning: Received nil payload for topic:", topic)
                goto continue
            end

            local sender = get_sender_info(msg)

            if topic == "ws.join" then
                -- Extract client_pid from the payload
                local client_pid = extract_client_pid(payload)

                if not client_pid then
                    print("Warning: Could not extract valid client PID from join payload")
                    goto continue
                end

                -- Ensure client_pid is a string
                client_pid = tostring(client_pid)

                print("WebSocket client joined:", client_pid)

                -- Add client to our list with a valid key
                state.connected_clients[client_pid] = {
                    force = { x = 0, y = 0 }
                }
                state.client_count = state.client_count + 1

                -- Send welcome message to this client with current blob state
                local ok, err = process.send(client_pid, "ws.message", {
                    type = "welcome",
                    message = "Welcome to the Blob Game!",
                    clients = state.client_count,
                    position = state.blob.position,
                    velocity = state.blob.velocity
                })

                if not ok then
                    print("Error sending welcome message:", err)
                end

            elseif topic == "ws.leave" then
                -- Extract client_pid from the payload
                local client_pid = extract_client_pid(payload)

                if not client_pid then
                    print("Warning: Could not extract valid client PID from leave payload")
                    goto continue
                end

                -- Ensure client_pid is a string
                client_pid = tostring(client_pid)

                print("WebSocket client left:", client_pid)

                -- Remove client from our list if it exists
                if state.connected_clients[client_pid] then
                    state.connected_clients[client_pid] = nil
                    state.client_count = state.client_count - 1
                end

            elseif topic == "ws.message" then
                -- Parse the message payload
                local message_data = payload

                -- Handle string messages (need to parse JSON)
                if type(message_data) == "string" then
                    local parsed, err = json.decode(message_data)
                    if err then
                        print("Error parsing JSON message:", err)
                        goto continue
                    end
                    message_data = parsed
                end

                -- Process different message types
                if type(message_data) == "table" then
                    if message_data.type == "force" then
                        -- It's a force update from client
                        process_force_update(sender, message_data)
                    elseif message_data.type == "canvas_info" then
                        -- Client is sending its canvas dimensions - update boundaries
                        if message_data.width and message_data.height then
                            local width = tonumber(message_data.width)
                            local height = tonumber(message_data.height)

                            if width and height then
                                -- Update blob position if this is the first client
                                if state.client_count == 1 then
                                    state.blob.position = {
                                        x = width / 2,
                                        y = height / 2
                                    }
                                end

                                -- Set boundaries to keep blob within the visible area
                                state.blob.boundary = {
                                    x_min = state.blob.radius,
                                    x_max = width - state.blob.radius,
                                    y_min = state.blob.radius,
                                    y_max = height - state.blob.radius
                                }

                                print(string.format("Updated boundaries to (%d,%d)-(%d,%d)",
                                    state.blob.boundary.x_min, state.blob.boundary.y_min,
                                    state.blob.boundary.x_max, state.blob.boundary.y_max))

                                -- Send immediate position update to this client
                                process.send(sender, "ws.message", {
                                    type = "update",
                                    clients = state.client_count,
                                    position = state.blob.position,
                                    velocity = state.blob.velocity
                                })
                            end
                        end
                    else
                        print("Received unknown message type:", message_data.type)
                    end
                else
                    print("Received unknown message:", type(message_data) == "table" and "table" or tostring(message_data))
                end
            end

        elseif result.channel == ticker:channel() then
            -- On ticker event, update physics and broadcast position
            update_physics()
            broadcast_position()

        elseif result.channel == events then
            -- Process a system event
            local event = result.value

            if event.kind == process.event.CANCEL then
                print("Blob Game Hub received cancel request")
                running = false
            elseif event.kind == process.event.LINK_DOWN then
                print("Link down event from:", event.event and event.event.from or "unknown")
            end
        end

        ::continue::
    end

    -- Cleanup before exit
    ticker:stop()

    print("Blob Game Hub shutting down")
    return { status = "shutdown", clients = state.client_count }
end

return { run = run }