local actor = require("actor")
local time = require("time")
local funcs = require("funcs")

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
        pid = process.pid(),
        count = 0
    }
    print("Message listener process started with PID>>>>>>>>>>>>>>>>>>>>>>>:", state.pid)
    process.registry.register("message_receiver")

    local fn = funcs.new()

    local message_actor = actor.new(state, {
        -- Handler for the dedicated message topic
        message = function(state, msg)
            state.count = state.count + 1

            -- Call the child module to get local time
            --local child_result, child_err = fn:call("app.demos.message_sending:child.get_local_time")
            local response_message = ""

            if child_err then
                response_message = "Error from child: " .. tostring(child_err)
            else
                response_message = "Child response: " .. tostring(child_result)
            end

            if type(msg) == "table" and msg.from then
                process.send(msg.from, "response", {
                    response_message .. " (Original message: " .. inspect_table(msg.payload) .. ")"
                })
            end
        end,

        __on_cancel = function(state)
            print("Process received cancel signal")
            return actor.exit({ status = "shutdown" })
        end,

        -- Default handler for any other topics via inbox
        __default = function(state, msg, topic)
            if msg.from then
                process.send(msg.from, "response", {
                    " (Original inbox message with topic '" ..
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
