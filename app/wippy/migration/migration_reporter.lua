local migration_reporter = {}
local time = require("time")

-- Event types for migration operations
migration_reporter.event = {
    DISCOVER = "migration:discover",
    PLAN = "migration:plan",
    START = "migration:start",
    SQL_EXECUTE = "migration:sql:execute",
    SQL_QUERY = "migration:sql:query",
    SQL_SUCCESS = "migration:sql:success",
    SQL_ERROR = "migration:sql:error",
    TX_BEGIN = "migration:tx:begin",
    TX_COMMIT = "migration:tx:commit",
    TX_ROLLBACK = "migration:tx:rollback",
    APPLIED = "migration:applied",
    SKIPPED = "migration:skipped",
    FAILED = "migration:failed",
    COMPLETE = "migration:complete",
    ERROR = "migration:error"
}

-- Create a reporter that sends events to a target
function migration_reporter.create(options)
    options = options or {}

    local reporter = {
        target = options.target,
        pid = options.pid,
        topic = options.topic or "migration:update",
        ref_id = options.ref_id,
        enabled = true
    }

    -- Report an event
    function reporter.report(event_type, data)
        if not reporter.enabled then return end

        -- Don't continue if no target
        if not reporter.target then return end

        -- Add ref_id if available
        if reporter.ref_id and not data.ref_id then
            data.ref_id = reporter.ref_id
        end

        -- Format message according to expected format
        local message = {
            type = event_type,
            data = data
        }

        -- Handle different target types
        if type(reporter.target) == "function" then
            -- Function target
            reporter.target(message)
        elseif type(reporter.target) == "table" and reporter.target.send then
            -- Channel-like target
            reporter.target:send(message)
        elseif reporter.pid and process and process.send then
            -- Process target
            process.send(reporter.pid, reporter.topic, message)
        end
    end

    -- Get current time
    function reporter.now()
        return time.now():unix()
    end

    -- Calculate duration between two timestamps
    function reporter.duration(start_time, end_time)
        return end_time - start_time
    end

    -- Enable reporting
    function reporter.enable()
        reporter.enabled = true
    end

    -- Disable reporting
    function reporter.disable()
        reporter.enabled = false
    end

    return reporter
end

-- Create a no-op reporter that doesn't send events
function migration_reporter.create_noop()
    return {
        report = function() end,
        now = function() return time.now():unix() end,
        duration = function(start, end_time) return end_time - start end,
        enable = function() end,
        disable = function() end
    }
end

return migration_reporter
