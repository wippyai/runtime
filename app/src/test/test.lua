local http = require("http")
local registry = require("registry")
local funcs = require("funcs")
local json = require("json")
local time = require("time")

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

    -- Create inbox for test messages
    local inbox = process.inbox()
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

    -- Configure test options
    local options = {
        pid = process.pid(),
        topic = "test:update",
        timeout = req:query("timeout") or "1m",
        msg_timeout = req:query("timeout") or "1s",
        query = { ["type"] = "test" }
    }

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

    -- Step 1: Discover available tests
    local entries, err = registry.find(options.query)
    if err or not entries or #entries == 0 then
        write_event("test:error", {
            message = err or "No test functions found",
            context = "test_discovery",
            timestamp = time.now():unix()
        })
        return false
    end

    -- Process test entries
    local tests = {}
    for i, entry in ipairs(entries) do
        local meta = entry.meta or {}
        local display_name = meta.name or ("Unnamed test " .. i)
        local group = meta.group or "default"

        table.insert(tests, {
            id = entry.id,
            name = display_name,
            group = group,
            meta = meta
        })
    end

    -- Sort tests by group and name
    table.sort(tests, function(a, b)
        if a.group ~= b.group then
            return a.group < b.group
        else
            return a.name < b.name
        end
    end)

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
        print("POLL TASKS >>>>>>>>>>>>>>")
            local result = channel.select {
                inbox:case_receive(),
                done_ch:case_receive()
            }
print("<<<<<<<<<<<<< GOT MSG!", json.encode(result))
            if not result.ok then break end

            local msg = result.value.payload

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

        local task, err = executor:async(test_id, test_options)
        if err then
            write_event("test:error", {
                message = "Failed to start test: " .. err,
                context = test_id,
                timestamp = time.now():unix()
            })

            tests_failed = tests_failed + 1
        else
            local ok = wait_for_completion(task:response(), options.timeout)
            if not ok then
                write_event("test:error", {
                    message = "Test timed out after " .. options.timeout,
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
