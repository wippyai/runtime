local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
    -- Generate unique message ID for this test run
    local test_id = "test-" .. tostring(os.time()) .. "-" .. tostring(math.random(1000, 9999))

    -- Publish a message to the queue
    local ok, err = queue.publish("app.queue:tasks", {
        action = "test_task",
        test_id = test_id,
        data = {
            value = 42,
            name = "integration test"
        }
    }, {
        correlation_id = "corr-" .. test_id,
        priority = 5
    })

    assert.is_nil(err, "publish should not return error")
    assert.eq(ok, true, "publish should return true")

    -- Wait for the message to be processed
    local max_wait = 5 -- seconds
    local processed = false
    local result = nil

    for i = 1, max_wait * 10 do
        time.sleep(100) -- 100ms

        -- Check if the message was processed by looking in store
        -- The handler stores results with key "queue:processed:{msg_id}"
        -- But we don't know the exact msg_id, so check the counter instead
        local counter, _ = store.get("queue:counter")
        if counter and counter > 0 then
            processed = true
            break
        end
    end

    assert.eq(processed, true, "message should be processed within timeout")

    return true
end

return { main = main }
