local time = require("time")
local json = require("json")
local logger = require("logger")
local websocket = require("websocket")
local env = require("env")
local http = require("http")

local DISCORD_GATEWAY = "wss://gateway.discord.gg/?v=10&encoding=json"
local INTENTS = 33281 -- GUILDS + GUILD_MESSAGES + MESSAGE_CONTENT

function websocket_demo()
    -- Set up response and request handling
    local res = http.response()
    local req = http.request()

    -- Initialize logger
    local log = logger:named("discord_bot")
    log:info("Starting Discord bot handler")

    -- Validate websocket request
    if not req:header("Upgrade") or string.lower(req:header("Upgrade")) ~= "websocket" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "This endpoint requires a WebSocket upgrade"
        })
        return
    end

    -- Get bot token from environment
    local token = env.get("DISCORD_BOT_TOKEN")
    if not token then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "No DISCORD_BOT_TOKEN found in environment"
        })
        return
    end

    log:info("Token validation", { length = #token })

    -- Set up response for WebSocket
    res:set_transfer(http.TRANSFER.CHUNKED)
    res:set_content_type(http.CONTENT.JSON)

    -- Set up WebSocket connection with proper options
    local client, ws_err = websocket.connect(DISCORD_GATEWAY, {
        headers = {
            ["User-Agent"] = "DiscordBot (Lua, 1.0)",
            ["Accept-Encoding"] = "gzip, deflate",
            ["Connection"] = "Upgrade",
            ["Upgrade"] = "websocket"
        },
        dial_timeout = time.parse_duration("5s"),
        read_timeout = time.parse_duration("30s"),
        write_timeout = time.parse_duration("10s")
    })

    if not client then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "WebSocket connection failed",
            details = ws_err
        })
        return
    end

    log:info("WebSocket connection established")

    -- Set up connection state
    local sequence = nil
    local heartbeat_interval = nil
    local identified = false
    local heartbeat_timer = nil
    local last_heartbeat_ack = true
    local connection_alive = true

    -- Handle identify payload
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
                    activities = {
                        {
                            name = "messages",
                            type = 3 -- Watching
                        }
                    }
                }
            }
        }

        local payload_str, encode_err = json.encode(identify_payload)
        if encode_err then
            log:error("Failed to encode identify payload", { error = encode_err })
            return false
        end

        log:info("Sending identify payload", { payload_length = #payload_str })

        local ok, send_err = client:send(payload_str)
        if not ok then
            log:error("Failed to send identify", { error = send_err })
            return false
        end

        log:info("Identify payload sent successfully")
        return true
    end

    -- Set up heartbeat handler
    local function start_heartbeat(interval)
        log:info("Starting heartbeat with interval", { interval_ms = interval })

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
                    break
                end

                last_heartbeat_ack = false
                log:debug("Sending heartbeat", { sequence = sequence })

                local heartbeat_payload = json.encode({
                    op = 1,
                    d = sequence
                })

                local ok, err = client:send(heartbeat_payload)
                if err then
                    log:error("Failed to send heartbeat", { error = err })
                    connection_alive = false
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
            break
        end

        -- Handle different message types
        if msg.type == websocket.TYPE_TEXT then
            local success, data = pcall(json.decode, msg.data)
            if not success then
                log:error("Failed to decode message", { error = data })
                goto continue
            end

            -- Handle various Discord gateway opcodes
            if data.op == 10 then -- Hello
                heartbeat_interval = data.d.heartbeat_interval
                start_heartbeat(heartbeat_interval)
                if not identified then
                    if not send_identify() then
                        break
                    end
                end
            elseif data.op == 11 then -- Heartbeat ACK
                last_heartbeat_ack = true
            elseif data.op == 0 then  -- Dispatch
                sequence = data.s

                if data.t == "READY" then
                    identified = true
                    log:info("Bot is ready!", {
                        user = data.d.user.username,
                        id = data.d.user.id,
                        guilds = #data.d.guilds
                    })

                    -- Send ready status to client
                    res:write_json({
                        status = "ready",
                        user = data.d.user.username,
                        guilds = #data.d.guilds
                    })
                    res:flush()
                end
            end
        elseif msg.type == websocket.TYPE_CLOSE then
            log:info("Received WebSocket close frame", {
                code = msg.code,
                reason = msg.reason
            })
            break
        end

        ::continue::
    end

    -- Cleanup
    log:info("Cleaning up connection")
    if heartbeat_timer then
        heartbeat_timer:stop()
    end

    -- Close WebSocket connection
    pcall(function()
        client:close(websocket.CLOSE_CODES.NORMAL, "Bot shutting down")
    end)

    log:info("Discord bot handler completed")
end
