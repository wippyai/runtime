local http = require("http")
local funcs = require("funcs")
local json = require("json")
local time = require("time")
local registry = require("registry")

-- Simplified test endpoint that combines test discovery and execution
local function run_tests()
    -- Set up HTTP response with chunked transfer
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to create HTTP context"
    end

    -- Set response headers for streaming JSON
    res:set_transfer(http.TRANSFER.CHUNKED)
    res:set_status(http.STATUS.OK)
    res:set_content_type(http.CONTENT.JSON)
    res:set_header("Access-Control-Allow-Origin", "*")
    res:set_header("Access-Control-Allow-Methods", "GET")

    -- Configure test options
    local options = {
        pid = process.pid(),
        topic = "test:update",
        timeout = req:query("timeout") or "15m",
        msg_timeout = req:query("timeout") or "1s",
        type = "test"
    }

    -- Apply filter options if provided
    if req:query("group") then
        options.group = req:query("group")
    end

    if req:query("tags") then
        options.tags = {}
        for tag in req:query("tags"):gmatch("([^,]+)") do
            table.insert(options.tags, tag:trim())
        end
    end

    -- Create inbox for test messages
    local inbox = process.listen("test:update")
    if not inbox then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            type = "test:error",
            data = {
                message = "Failed to create process inbox",
                timestamp = time.now():unix()
            }
        })
        return false
    end

    -- Utility function to send events to the client
    local function write_event(type, data)
        res:write_json({
            type = type,
            data = data
        })
        res:write("\n")
        res:flush()
    end

    -- Helper functions that waits for suite completion event
    local function wait_for_completion(on_chan, timeout)
        local result = channel.select {
            on_chan:case_receive(),
            time.after(timeout):case_receive()
        }
        if result.channel == on_chan then
            return result.value
        end
        return false
    end

    -- Step 1: Discover available tests using the test_registry library
    local tests, err = registry.find(options)
    if err or not tests or #tests == 0 then
        write_event("test:error", {
            message = err or "No test functions found",
            context = "test_discovery",
            timestamp = time.now():unix()
        })
        return false
    end

    -- Send discovered tests to client
    write_event("test:discover", {
        tests = tests
    })

    local done_ch = channel.new()
    local test_done_ch = channel.new(1)
    local wait = channel.new(1)

    ---- Message processor coroutine
    coroutine.spawn(function()
        while true do
            local result = channel.select {
                inbox:case_receive(),
                done_ch:case_receive()
            }

            if not result.ok then break end

            -- messages in inbox are wrapped in a message object
            local msg = result.value

            if msg.type == "test:complete" then
                test_done_ch:send(msg)
            end

            write_event(msg.type, msg.data)
        end

        wait:send(true)
    end)

    -- Create function executor
    local executor = funcs.new()

    -- Track test execution metrics
    local tests_completed = 0
    local tests_failed = 0

    -- Execute each test function
    for _, test_info in ipairs(tests) do
        local test_id = test_info.id

        -- Notify client that test is starting
        write_event("test:suite:start", {
            id = test_id,
            name = test_info.name,
            group = test_info.group,
            time = time.now():unix()
        })

        -- Execute the test function
        local test_options = {
            pid = options.pid,
            topic = options.topic,
            ref_id = test_id
        }

        local cmd, err = executor:async(test_id, test_options)
        if err then
            write_event("test:error", {
                message = "Failed to start test: " .. err,
                context = test_id,
                timestamp = time.now():unix()
            })

            tests_failed = tests_failed + 1
        else
            local ok = wait_for_completion(cmd:response(), options.timeout)
            if not ok then
                local _, err = cmd:result()
                if err == nil then
                   err = "Test timed out after " .. options.timeout
                end

                write_event("test:error", {
                    message = tostring(err),
                    context = test_id,
                    timestamp = time.now():unix()
                })

                tests_failed = tests_failed + 1
            else
                -- we receive events in async, wait for completion marker, might be a little delayed
                wait_for_completion(test_done_ch, options.msg_timeout)
            end
        end

        tests_completed = tests_completed + 1
    end

    done_ch:close()

    if not wait_for_completion(wait, "100ms") then
        write_event("test:error", {
            message = "Failed to wait for completion",
            context = "test_completion",
            timestamp = time.now():unix()
        })
    end

    -- Send final summary
    write_event("test:summary", {
        total = #tests,
        completed = tests_completed,
        failed = tests_failed,
        status = tests_failed > 0 and "failed" or "passed",
        timestamp = time.now():unix()
    })

    return true
end

return { run_tests = run_tests }
