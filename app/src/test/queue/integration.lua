local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
    -- Get store instance (same one used by task_handler)
    local s, store_err = store.get("app.test.store:memory")
    assert.is_nil(store_err, "should get store")

    -- Clear counter before test
    s:delete("queue:counter")

    -- Publish a message
    local ok, err = queue.publish("app.queue:tasks", {
        action = "integration_test",
        timestamp = tostring(time.now():unix_nano())
    })

    assert.is_nil(err, "publish should not return error")
    assert.eq(ok, true, "publish should return true")

    -- Poll for consumer to process message
    local processed = false
    for i = 1, 50 do
        time.sleep("50ms")
        local counter, _ = s:get("queue:counter")
        if counter and counter > 0 then
            processed = true
            break
        end
    end

    assert.eq(processed, true, "message should be processed by consumer")

    return true
end

return { main = main }
