local M = {}

-- Discord constants
local DISCORD_GATEWAY = "wss://gateway.discord.gg/?v=10&encoding=json"
local DISCORD_API = "https://discord.com/api/v10"
local OPCODES = {
    DISPATCH = 0,
    HEARTBEAT = 1,
    IDENTIFY = 2,
    PRESENCE_UPDATE = 3,
    HELLO = 10,
    HEARTBEAT_ACK = 11
}

M.Client = {
    new = function(token, options)
        options = options or {}
        local callbacks = {}
        local sequence = nil
        local identified = false
        local heartbeat_timer = nil
        local last_heartbeat_time = nil
        local last_ack_received = true
        local channels = {}
        local active_channel = nil

        -- Default intents if not specified
        local intents = options.intents or 65281 -- GUILDS + GUILD_MESSAGES + MESSAGE_CONTENT

        local function trigger_callback(event, data)
            if callbacks[event] then
                callbacks[event](data)
            end
        end

        local function make_api_request(method, endpoint, body)
            local headers = {
                ["Authorization"] = "Bot " .. token,
                ["Content-Type"] = "application/json"
            }

            local url = DISCORD_API .. endpoint

            local response, err = http.request(method, url, {
                headers = headers,
                body = body and json.encode(body) or nil
            })

            if err then
                return nil, err
            end

            if response.status_code >= 200 and response.status_code < 300 then
                return json.decode(response.body), nil
            else
                return nil, response.body
            end
        end

        local function handle_heartbeat(client)
            local heartbeat_count = 0
            return function()
                if not last_ack_received then
                    trigger_callback("error", "Heartbeat ACK timeout")
                    return false
                end

                heartbeat_count = heartbeat_count + 1
                last_heartbeat_time = time.now()

                local payload = {
                    op = OPCODES.HEARTBEAT,
                    d = sequence
                }

                local success = client:send(json.encode(payload))
                if not success then
                    trigger_callback("error", "Failed to send heartbeat")
                    return false
                end

                last_ack_received = false
                return true
            end
        end

        return {
            -- Register event handlers
            on = function(self, event, callback)
                callbacks[event] = callback
                return self
            end,

            -- Get list of channels in a guild
            get_channels = function(self, guild_id)
                local data, err = make_api_request("GET", "/guilds/" .. guild_id .. "/channels")
                if err then
                    trigger_callback("error", "Failed to fetch channels: " .. err)
                    return nil
                end

                -- Store channels for later use
                channels = {}
                for _, channel in ipairs(data) do
                    if channel.type == 0 then -- Text channels only
                        channels[channel.id] = channel
                    end
                end

                return channels
            end,

            -- Set active channel for listening
            set_active_channel = function(self, channel_id)
                if channels[channel_id] then
                    active_channel = channel_id
                    trigger_callback("channel_change", channels[channel_id])
                    return true
                end
                return false
            end,

            -- Send message to a channel
            send_message = function(self, channel_id, content)
                local data, err = make_api_request("POST", "/channels/" .. channel_id .. "/messages", {
                    content = content
                })
                if err then
                    trigger_callback("error", "Failed to send message: " .. err)
                    return false
                end
                return true
            end,

            -- Set bot presence
            set_presence = function(self, client, status, activity_type, activity_name)
                local presence_payload = {
                    op = OPCODES.PRESENCE_UPDATE,
                    d = {
                        since = time.now():unix_nano() / 1000000,
                        activities = { {
                            name = activity_name or "Watching channels",
                            type = activity_type or 3 -- 3 is "Watching"
                        } },
                        status = status or "online",
                        afk = false
                    }
                }
                return client:send(json.encode(presence_payload))
            end,

            -- Start the client
            start = function(self)
                if not token then
                    trigger_callback("error", "No token provided")
                    return
                end

                coroutine.spawn(function()
                    while true do
                        local client, err = websocket.connect(DISCORD_GATEWAY, {
                            headers = {
                                ["User-Agent"] = "DiscordBot (Lua, 1.0)"
                            }
                        })

                        if not client then
                            trigger_callback("error", "Connection failed: " .. tostring(err))
                            time.sleep(time.parse_duration("5s"))
                            goto continue
                        end

                        trigger_callback("connect")
                        local receive_ch = client:receive()

                        while true do
                            local msg, ok = receive_ch:receive()
                            if not ok then break end

                            if msg.type == websocket.TYPE_TEXT then
                                local success, data = pcall(json.decode, msg.data)
                                if not success then goto continue end

                                -- Handle different opcodes
                                if data.op == OPCODES.HELLO then
                                    local interval = math.floor(data.d.heartbeat_interval / 2)

                                    if heartbeat_timer then
                                        heartbeat_timer:stop()
                                    end

                                    heartbeat_timer = time.ticker(interval)

                                    -- Start heartbeat routine
                                    coroutine.spawn(function()
                                        local heartbeat = handle_heartbeat(client)
                                        local heartbeat_ch = heartbeat_timer:channel()

                                        while true do
                                            local _, ok = heartbeat_ch:receive()
                                            if not ok or not heartbeat() then
                                                break
                                            end
                                        end
                                    end)

                                    -- Send identify if not already identified
                                    if not identified then
                                        local identify_payload = {
                                            op = OPCODES.IDENTIFY,
                                            d = {
                                                token = token,
                                                intents = intents,
                                                properties = {
                                                    os = "linux",
                                                    browser = "lua",
                                                    device = "lua"
                                                },
                                                presence = {
                                                    activities = { {
                                                        name = "Watching channels",
                                                        type = 3
                                                    } },
                                                    status = "online",
                                                    since = time.now():unix_nano() / 1000000,
                                                    afk = false
                                                }
                                            }
                                        }
                                        client:send(json.encode(identify_payload))
                                    end
                                elseif data.op == OPCODES.HEARTBEAT_ACK then
                                    last_ack_received = true
                                    trigger_callback("heartbeat_ack")
                                elseif data.op == OPCODES.DISPATCH then
                                    sequence = data.s

                                    -- Handle different event types
                                    if data.t == "READY" then
                                        identified = true
                                        trigger_callback("ready", data.d)
                                    elseif data.t == "MESSAGE_CREATE" then
                                        -- Only trigger for active channel if set
                                        if not active_channel or data.d.channel_id == active_channel then
                                            trigger_callback("message", data.d)
                                        end
                                    end

                                    -- Trigger raw dispatch event for custom handling
                                    trigger_callback("dispatch", data)
                                end
                            end
                            ::continue::
                        end

                        if heartbeat_timer then
                            heartbeat_timer:stop()
                        end

                        -- Clean disconnect
                        pcall(function()
                            client:close(1000, "Bot shutting down")
                        end)

                        trigger_callback("disconnect")
                        time.sleep(time.parse_duration("5s"))
                        ::continue::
                    end
                end)

                return self
            end,

            -- Get current active channel
            get_active_channel = function(self)
                if active_channel and channels[active_channel] then
                    return channels[active_channel]
                end
                return nil
            end
        }
    end
}

return M
