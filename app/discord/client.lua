local time = require("time")
local json = require("json")
local http = require("http_client")
local websocket = require("websocket")

-- Discord protocol constants
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

local Client = {}
Client.__index = Client

function Client.new(token, options)
    local self = setmetatable({}, Client)
    self.token = token
    self.options = options or {}
    self.callbacks = {}
    self.sequence = nil
    self.identified = false
    self.heartbeat_timer = nil
    self.last_heartbeat_time = nil
    self.last_ack_received = true
    self.channels = {}
    self.active_channel = nil
    self.active_guild = nil
    self.last_active_channel = nil
    self.intents = self.options.intents or 65281
    self.application_id = nil

    return self
end

function Client:preserve_channel_state()
    if self.active_channel then
        self.last_active_channel = {
            id = self.active_channel,
            guild_id = self.active_guild,
            channel_data = self.channels[self.active_channel]
        }
    end
end

function Client:trigger_callback(event, data)
    if self.callbacks[event] then
        self.callbacks[event](data)
    end
end

function Client:make_api_request(method, endpoint, body)
    local headers = {
        ["Authorization"] = "Bot " .. self.token,
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

function Client:register_commands(commands, guild_id)
    -- Get application ID first if we don't have it
    if not self.application_id then
        local app_data, err = self:make_api_request("GET", "/oauth2/applications/@me")
        if err then
            self:trigger_callback("error", "Failed to get application data: " .. err)
            return false
        end
        self.application_id = app_data.id
    end

    -- Endpoint depends on if we're registering globally or for specific guild
    local endpoint = guild_id
        and string.format("/applications/%s/guilds/%s/commands", self.application_id, guild_id)
        or string.format("/applications/%s/commands", self.application_id)

    -- Register each command
    for _, command in ipairs(commands) do
        local data, err = self:make_api_request("POST", endpoint, command)
        if err then
            self:trigger_callback("error", "Failed to register command " .. command.name .. ": " .. err)
        else
            self:trigger_callback("command_registered", command.name)
        end
    end
end

function Client:create_interaction_response(interaction_id, interaction_token, response_data)
    local endpoint = string.format("/interactions/%s/%s/callback", interaction_id, interaction_token)
    return self:make_api_request("POST", endpoint, response_data)
end

function Client:edit_interaction_response(interaction_token, message_data)
    local endpoint = string.format("/webhooks/%s/%s/messages/@original", self.application_id, interaction_token)
    return self:make_api_request("PATCH", endpoint, message_data)
end

function Client:handle_interaction(interaction)
    if interaction.type == 2 then  -- Type 2 is APPLICATION_COMMAND
        if interaction.data.name == "help" then
            self:create_interaction_response(interaction.id, interaction.token, {
                type = 4,  -- CHANNEL_MESSAGE_WITH_SOURCE
                data = {
                    content = "Available commands:\n" ..
                             "/help - Show this help message\n" ..
                             "/ask - Ask the AI a question\n" ..
                             "/explain - Get an explanation of a topic"
                }
            })
        elseif interaction.data.name == "ask" then
            local question = interaction.data.options[1].value
            -- Defer the response since AI might take a while
            self:create_interaction_response(interaction.id, interaction.token, {
                type = 5,  -- DEFERRED_CHANNEL_MESSAGE
            })

            -- Get AI response
            local result, err = self.app.llm_client:query_direct(question, {})

            -- Edit the deferred response with the result
            self:edit_interaction_response(interaction.token, {
                content = err and ("Error: " .. err) or result
            })
        end
    end
end

function Client:handle_heartbeat(ws_client)
    local heartbeat_count = 0
    return function()
        if not self.last_ack_received then
            self:trigger_callback("error", "Heartbeat ACK timeout")
            return false
        end

        heartbeat_count = heartbeat_count + 1
        self.last_heartbeat_time = time.now()

        local payload = {
            op = OPCODES.HEARTBEAT,
            d = self.sequence
        }

        local success = ws_client:send(json.encode(payload))
        if not success then
            self:trigger_callback("error", "Failed to send heartbeat")
            return false
        end

        self.last_ack_received = false
        return true
    end
end

function Client:handle_attachments(message)
    if message.attachments and #message.attachments > 0 then
        for _, attachment in ipairs(message.attachments) do
            -- Get the file extension
            local filename = attachment.filename
            local extension = filename:match("^.+(%..+)$")

            if extension then
                -- Log the file detection
                self:trigger_callback("file_detected", {
                    filename = filename,
                    url = attachment.url,
                    size = attachment.size,
                    content_type = attachment.content_type,
                    channel_id = message.channel_id,
                    author = message.author.username
                })
            end
        end
    end
end

function Client:handle_disconnect()
    self:preserve_channel_state()
    if self.heartbeat_timer then
        self.heartbeat_timer:stop()
    end
    self.identified = false
    self:trigger_callback("disconnect")
end

function Client:on(event, callback)
    self.callbacks[event] = callback
    return self
end

function Client:get_channels(guild_id)
    local data, err = self:make_api_request("GET", "/guilds/" .. guild_id .. "/channels")
    if err then
        self:trigger_callback("error", "Failed to fetch channels: " .. err)
        return nil
    end

    -- Store text channels for later use
    self.channels = {}
    for _, channel in ipairs(data) do
        if channel.type == 0 then -- Text channels only
            self.channels[channel.id] = channel
        end
    end

    return self.channels
end

function Client:set_active_channel(channel_id)
    if self.channels[channel_id] then
        self.active_channel = channel_id
        self:preserve_channel_state()  -- Save the state when changing channels
        self:trigger_callback("channel_change", self.channels[channel_id])
        return true
    end
    return false
end

function Client:send_message(channel_id, content)
    local data, err = self:make_api_request("POST", "/channels/" .. channel_id .. "/messages", {
        content = content
    })
    if err then
        self:trigger_callback("error", "Failed to send message: " .. err)
        return false
    end
    return true
end

function Client:set_presence(ws_client, status, activity_type, activity_name)
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
    return ws_client:send(json.encode(presence_payload))
end

function Client:start()
    if not self.token then
        self:trigger_callback("error", "No token provided")
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
                self:trigger_callback("error", "Connection failed: " .. tostring(err))
                time.sleep(time.parse_duration("5s"))
                goto continue
            end

            self:trigger_callback("connect")
            local receive_ch = client:receive()

            while true do
                local msg, ok = receive_ch:receive()
                if not ok then
                    self:handle_disconnect()
                    break
                end

                if msg.type == websocket.TYPE_TEXT then
                    local success, data = pcall(json.decode, msg.data)
                    if not success then goto continue end

                    -- Handle different opcodes
                    if data.op == OPCODES.HELLO then
                        local interval = math.floor(data.d.heartbeat_interval / 2)

                        if self.heartbeat_timer then
                            self.heartbeat_timer:stop()
                        end

                        self.heartbeat_timer = time.ticker(interval)

                        -- Start heartbeat routine
                        coroutine.spawn(function()
                            local heartbeat = self:handle_heartbeat(client)
                            local heartbeat_ch = self.heartbeat_timer:channel()

                            while true do
                                local _, ok = heartbeat_ch:receive()
                                if not ok or not heartbeat() then
                                    break
                                end
                            end
                        end)

                        -- Send identify if not already identified
                        if not self.identified then
                            local identify_payload = {
                                op = OPCODES.IDENTIFY,
                                d = {
                                    token = self.token,
                                    intents = self.intents,
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
                        self.last_ack_received = true
                        self:trigger_callback("heartbeat_ack")
                    elseif data.op == OPCODES.DISPATCH then
                        self.sequence = data.s

                        -- Handle different event types
                        if data.t == "READY" then
                            self.identified = true
                            self:trigger_callback("ready", data.d)

                            -- Restore previous channel state if it exists
                            if self.last_active_channel then
                                coroutine.spawn(function()
                                    -- Wait a short time for guild data to be available
                                    time.sleep(time.parse_duration("2s"))

                                    -- Refetch channels for the guild
                                    local channels = self:get_channels(self.last_active_channel.guild_id)
                                    if channels then
                                        -- Restore the active channel
                                        self:set_active_channel(self.last_active_channel.id)
                                    end
                                end)
                            end
                        elseif data.t == "MESSAGE_CREATE" then
                            -- Only trigger for active channel if set
                            if not self.active_channel or data.d.channel_id == self.active_channel then
                                self:trigger_callback("message", data.d)
                            end
                        end

                        -- Trigger raw dispatch event for custom handling
                        self:trigger_callback("dispatch", data)
                    end
                end
                ::continue::
            end

            -- Clean disconnect and cleanup
            pcall(function()
                client:close(1000, "Bot shutting down")
            end)

            time.sleep(time.parse_duration("5s"))
            ::continue::
        end
    end)

    return self
end

function Client:get_active_channel()
    if self.active_channel and self.channels[self.active_channel] then
        return self.channels[self.active_channel]
    end
    return nil
end

return {
    Client = Client
}