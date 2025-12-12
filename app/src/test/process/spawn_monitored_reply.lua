-- Test: spawn_monitored with reply pattern
-- Reproduces the gov client/service pattern:
-- 1. Test spawns a service process
-- 2. Test creates a response channel
-- 3. Test sends message with respond_to field to service
-- 4. Service spawns monitored worker
-- 5. Worker exits with result
-- 6. Service gets EXIT event and sends reply to respond_to channel
-- 7. Test receives reply

local assert = require("assert2")
local time = require("time")
local uuid = require("uuid")

local function main()
    local events_ch = process.events()

    -- Spawn the service that will handle requests
    local service_pid, err = process.spawn_monitored(
        "app.test.process:spawn_monitored_service",
        "app:processes"
    )
    assert.is_nil(err, "spawn service no error")
    assert.not_nil(service_pid, "got service pid")

    -- Give service time to start and listen
    time.sleep("50ms")

    -- Create response channel (like gov client)
    -- Default is raw payloads - direct Lua table access
    local response_channel_name = "test.response." .. uuid.v4()
    local response_channel = process.listen(response_channel_name)
    assert.not_nil(response_channel, "created response channel")

    -- Send request to service (like gov client send_and_wait)
    local ok = process.send(service_pid, "request", {
        respond_to = response_channel_name,
        request_id = "test-request-1",
        work_data = "hello"
    })
    assert.ok(ok, "send to service succeeded")

    -- Wait for response with timeout
    local timeout = time.after("3s")
    local result = channel.select({
        response_channel:case_receive(),
        timeout:case_receive()
    })

    assert.ok(result.channel ~= timeout, "response received before timeout")
    assert.not_nil(result.value, "got response value")
    local response = result.value
    assert.eq(response.request_id, "test-request-1", "correct request_id")
    assert.ok(response.success, "response indicates success")
    assert.eq(response.result, "processed: hello", "worker processed data")

    return true
end

return { main = main }
