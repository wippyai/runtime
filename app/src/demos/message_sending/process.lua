local actor = require("actor")
local time = require("time")

local function inspect_table(t, depth)
    if type(t) ~= "table" then return tostring(t) end
    depth = depth or 0
    if depth > 5 then return "..." end

    local result = {}
    for k, v in pairs(t) do
        local val = type(v) == "table" and inspect_table(v, depth + 1) or tostring(v)
        table.insert(result, tostring(k) .. "=" .. val)
    end
    return "{" .. table.concat(result, ", ") .. "}"
end

local function run()
    local state = {
        pid = process.pid()
    }
    print("Message listener process started with PID:", state.pid)
    process.registry.register("message_receiver")

    local message_actor = actor.new(state, {
        -- Handler for the dedicated message topic
        message = function(state, msg)
            --print("Received message on 'message' topic:", inspect_table(msg))

            if type(msg) == "table" and msg.from then
                process.send(msg.from, "response", {
                    "Message received and processed on 'message' topic: " .. inspect_table(msg.payload)
                })
            end
        end,

        on_cancel = function(state)
            print("Process received cancel signal")
            return actor.exit({ status = "shutdown" })
        end,

        -- Default handler for any other topics via inbox
        __default = function(state, msg, topic)
            --print("Received message on default inbox topic '" .. tostring(topic) .. "':", "payload=", inspect_table(msg))

            if msg.from then
                process.send(msg.from, "response", {
                    "Message received and processed from inbox (original topic '" ..
                    topic .. "'): " .. inspect_table(msg.payload)
                })
            end
        end
    })

    local result = message_actor.run()
    print("Message listener process shutting down:", state.pid)
    return result
end

return { run = run }
