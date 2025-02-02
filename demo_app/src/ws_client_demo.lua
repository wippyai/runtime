local time = require("time")
local json = require("json")
local logger = require("logger")
local websocket = require("websocket")

local DISCORD_GATEWAY = "wss://gateway.discord.gg/?v=10&encoding=json"
local INTENTS = 33281  -- GUILDS + GUILD_MESSAGES + MESSAGE_CONTENT

function websocket_demo()
    local log = logger:named("discord_bot")
    log:info("Starting Discord bot handler")

    -- Get bot token from environment
    local token = "MTMzNTQ1NTMzODY0ODA0NzcxOA.GjgVAf.4aJWl-FVi6LeJSL-IuELs4uCW6W0_R8BDvpcXY"
    if not token then
        log:error("No Discord token found in environment")
        return
    end

    log:info("Token validation", {length = #token})

    local ws_channel = channel.new(100)
    local should_reconnect = true
    local active = true

    while active do
        should_reconnect = false
        log:info("Attempting to connect to Discord gateway")

        local client, err = websocket.connect(DISCORD_GATEWAY, {
            headers = {
                ["User-Agent"] = "DiscordBot (Lua, 1.0)"
            },
            dial_timeout = "5s",
            read_timeout = "30s",
            write_timeout = "10s"
        })

        if not client then
            log:error("WebSocket connection failed", {error = err})
            time.sleep(time.parse_duration("5s"))
            goto continue
        end

        log:info("WebSocket connection established")

        local sequence = nil
        local heartbeat_interval = nil
        local identified = false
        local heartbeat_timer = nil
        local last_heartbeat_ack = true
        local connection_alive = true

        local function send_identify()
            log:info("Preparing identify payload")

            local identify_payload = {
                op = 2,
                d = {
                    token = token,
                    intents = INTENTS,
                    properties = {
                        os = "linux",
                        browser = "lua",
                        device = "lua"
                    },
                    presence = {
                        status = "online",
                        activities = {{
                            name = "messages",
                            type = 3  -- Watching
                        }}
                    }
                }
            }

            local payload_str = json.encode(identify_payload)
            log:info("Sending identify payload", {payload_length = #payload_str})

            local ok, err = client:send(payload_str)
            if not ok then
                log:error("Failed to send identify", {error = err})
                return false
            end

            log:info("Identify payload sent successfully")
            return true
        end

        local function start_heartbeat(interval)
            log:info("Starting heartbeat with interval", {interval_ms = interval})

            if heartbeat_timer then
                heartbeat_timer:stop()
            end

            heartbeat_timer = time.ticker(interval)
            local ticker_ch = heartbeat_timer:channel()
            last_heartbeat_ack = true

            coroutine.spawn(function()
                while connection_alive do
                    local _, ok = ticker_ch:receive()
                    if not ok then
                        log:info("Heartbeat ticker stopped")
                        break
                    end

                    if not last_heartbeat_ack then
                        log:warn("Previous heartbeat not acknowledged, reconnecting")
                        connection_alive = false
                        should_reconnect = true
                        break
                    end

                    last_heartbeat_ack = false
                    log:debug("Sending heartbeat", {sequence = sequence})

                    local ok, err = client:send(json.encode({
                        op = 1,
                        d = sequence
                    }))

                    if not ok then
                        log:error("Failed to send heartbeat", {error = err})
                        connection_alive = false
                        should_reconnect = true
                        break
                    end
                end
            end)
        end

        -- Handle incoming messages
        local receive_ch = client:receive()

        while connection_alive do
            log:debug("Waiting for next message")
            local msg, ok = receive_ch:receive()

            if not ok then
                log:warn("WebSocket receive channel closed")
                connection_alive = false
                should_reconnect = true
                break
            end

            -- Log raw message for debugging
            log:info("Raw WebSocket message received", {
                type = msg.type,
                length = msg.data and #msg.data or 0
            })

            if msg.type == websocket.TYPE_TEXT then
                log:debug("Raw message data", {data = msg.data})
                local success, data = pcall(json.decode, msg.data)
                if not success then
                    log:error("Failed to decode message", {error = data, raw = msg.data})
                    goto continue
                end

                log:info("Gateway message", {
                    op = data.op,
                    type = data.t,
                    seq = data.s
                })

                if data.op == 10 then  -- Hello
                    log:info("Received Hello from gateway", {heartbeat_interval = data.d.heartbeat_interval})
                    heartbeat_interval = data.d.heartbeat_interval
                    start_heartbeat(heartbeat_interval)
                    if not identified then
                        if not send_identify() then
                            connection_alive = false
                            should_reconnect = true
                            break
                        end
                    end

                elseif data.op == 11 then  -- Heartbeat ACK
                    log:debug("Heartbeat ACK received")
                    last_heartbeat_ack = true

                elseif data.op == 9 then  -- Invalid Session
                    log:warn("Invalid session received, will need to re-identify", {resumable = data.d})
                    time.sleep(time.parse_duration("1s"))
                    identified = false
                    if not send_identify() then
                        connection_alive = false
                        should_reconnect = true
                        break
                    end

                elseif data.op == 7 then  -- Reconnect
                    log:info("Discord requested reconnect")
                    connection_alive = false
                    should_reconnect = true
                    break

                elseif data.op == 0 then  -- Dispatch
                    sequence = data.s

                    if data.t == "READY" then
                        identified = true
                        log:info("Bot is ready!", {
                            user = data.d.user.username,
                            id = data.d.user.id,
                            guilds = #data.d.guilds
                        })
                    elseif data.t == "MESSAGE_CREATE" then
                        if not data.d.author.bot then
                            log:info("Message received", {
                                content = data.d.content,
                                author = data.d.author.username,
                                channel_id = data.d.channel_id,
                                guild_id = data.d.guild_id
                            })
                        end
                    elseif data.t == "GUILD_CREATE" then
                        log:info("Joined guild", {
                            name = data.d.name,
                            id = data.d.id
                        })
                    end
                end
            elseif msg.type == websocket.TYPE_CLOSE then
                log:info("Received WebSocket close frame", {code = msg.code, reason = msg.reason})
                connection_alive = false
                should_reconnect = true
                break
            end

            ::continue::
        end

        -- Cleanup
        log:info("Cleaning up connection")
        if heartbeat_timer then
            heartbeat_timer:stop()
        end

        pcall(function()
            client:close(websocket.CLOSE_CODES.NORMAL, "Bot shutting down")
        end)

        if should_reconnect then
            log:info("Waiting before reconnect attempt")
            time.sleep(time.parse_duration("5s"))
        end

        ::continue::
    end

    log:info("Discord bot handler completed")
end