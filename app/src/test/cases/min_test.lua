local time = require("time")
local http = require("http_client")
local sql = require("sql")
local fs = require("fs")

-- Self-contained function that runs tests and preserves messaging capabilities
-- Includes basic testing functionality
local function run_minimal_test(options)
    -- Cache the original process.send at function call time
    local original_send = process and process.send

    -- Default options
    options = options or {}
    local topic = options.topic or "test:update"
    local target_pid = options.pid
    local ref_id = options.ref_id

    -- Message sending function using original process.send
    local function send_message(type, data)
        if not target_pid or not original_send then
            return -- No-op if we can't send messages
        end

        -- Add ref_id if available
        if ref_id and not data.ref_id then
            data.ref_id = ref_id
        end

        -- Send the message
        original_send(target_pid, topic, {
            type = type,
            data = data
        })
    end

    -- Event types
    local EVENT = {
        PLAN = "test:plan",
        CASE_START = "test:case:start",
        CASE_PASS = "test:case:pass",
        CASE_FAIL = "test:case:fail",
        COMPLETE = "test:complete"
    }

    -- Define the tests to run directly in this function
    local tests = {
        --{
        --    name = "http_get_test",
        --    fn = function()
        --        local response, err = http.get("http://localhost:8082/", {
        --            timeout = "5s"
        --        })
        --        assert(err == nil, "HTTP request failed: " .. (err or "unknown error"))
        --        assert(response ~= nil, "No response received")
        --        assert(response.status_code == 200, "Expected 200 status code")
        --    end
        --},
        --{
        --    name = "file_system_test",
        --    fn = function()
        --        local root_fs = fs.get("system:root")
        --        assert(root_fs ~= nil, "Failed to get root filesystem")
        --
        --        local entries = {}
        --        for entry in root_fs:readdir("./") do
        --            table.insert(entries, entry.name)
        --        end
        --        assert(#entries > 0, "Directory is empty or could not be read")
        --    end
        --},
        --{
        --    name = "database_test",
        --    fn = function()
        --        local db, err = sql.get("system:db")
        --        assert(err == nil, "Failed to connect to database: " .. (err or "unknown error"))
        --        assert(db ~= nil, "No database connection received")
        --
        --        local dbType, err = db:type()
        --        assert(err == nil, "Failed to get database type: " .. (err or "unknown error"))
        --        assert(dbType == sql.type.sqlite, "Expected SQLite database")
        --        db:release()
        --    end
        --}
    }

    -- Results tracking
    local results = {
        total = #tests,
        passed = 0,
        failed = 0,
        tests = {}
    }

    -- Report test plan
  --  send_message(EVENT.PLAN, {
  --      suites = {
  --          {
  --              name = "SystemTests",
  --              tests = tests
  --          }
  --      }
  --  })

    --local start_time = time.now()
    --
    ---- Run each test
    --for _, test in ipairs(tests) do
    --    local name = test.name
    --    local fn = test.fn
    --
    --    -- Notify test start
    --    local test_start = time.now()
    --    send_message(EVENT.CASE_START, {
    --        suite = "SystemTests",
    --        test = name,
    --        timestamp = test_start:unix()
    --    })
    --
    --    -- Run the test using cpcall for coroutine compatibility
    --    local success, err = cpcall(fn)
    --
    --    -- Calculate duration
    --    local test_end = time.now()
    --    local duration = test_end:sub(test_start):milliseconds() / 1000
    --
    --    -- Record result
    --    local result = {
    --        name = name,
    --        duration = duration,
    --        status = success and "pass" or "fail",
    --        error = not success and tostring(err) or nil
    --    }
    --
    --    table.insert(results.tests, result)
    --
    --    -- Send appropriate event
    --    if success then
    --        results.passed = results.passed + 1
    --        send_message(EVENT.CASE_PASS, {
    --            suite = "SystemTests",
    --            test = name,
    --            duration = duration,
    --            timestamp = test_end:unix()
    --        })
    --    else
    --        results.failed = results.failed + 1
    --        send_message(EVENT.CASE_FAIL, {
    --            suite = "SystemTests",
    --            test = name,
    --            duration = duration,
    --            error = tostring(err),
    --            timestamp = test_end:unix()
    --        })
    --    end
    --end
    --
    ---- Calculate total duration
    --local end_time = time.now()
    --local total_duration = end_time:sub(start_time):milliseconds() / 1000
    --
    ---- Send completion event
    print("results.failed", results.failed)
    send_message(EVENT.COMPLETE, {
     --   total = results.total,
     --   passed = results.passed,
    --    failed = results.failed,
       -- duration = total_duration,
      --  timestamp = end_time:unix(),
        status = results.failed > 0 and "failed" or "passed"
    })
    print("results.returned", results.failed)

    -- Return formatted results
    return {
        timestamp = time.now():unix(),
        status = results.failed > 0 and "error" or "ok",
        --total_tests = results.total,
        --passed_tests = results.passed,
        --failed_tests = results.failed,
       -- duration_ms = total_duration * 1000,
        --test_results = results.tests
    }
end

return run_minimal_test
