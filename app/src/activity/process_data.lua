-- process_data.lua
-- A minimal activity that processes simple input data

local json = require("json")
local activity = require("temporal_activity")

local function handler(input)
    -- Get activity info
    local info, err = activity.info()
    if err then
        print("Error getting activity info: " .. tostring(err))
    else
        print("Activity Info:")
        print("  Workflow ID: " .. (info.workflow_id or "unknown"))
        print("  Run ID: " .. (info.run_id or "unknown"))
        print("  Activity ID: " .. (info.activity_id or "unknown"))
        print("  Activity Type: " .. (info.activity_type or "unknown"))
        print("  Task Queue: " .. (info.task_queue or "unknown"))
        print("  Attempt: " .. tostring(info.attempt or 0))
        print("  Is Local: " .. tostring(info.is_local or false))
    end

    -- Check for heartbeat details (for retry/resume scenarios)
    if activity.has_heartbeat_details() then
        local details, err = activity.get_heartbeat_details()
        if not err then
            print("Resuming from heartbeat details: " .. json.encode(details))
        end
    end

    -- Process data with heartbeat
    print("Processing data with ID: " .. (input.id or "unknown"))

    -- Record heartbeat with progress
    activity.heartbeat({ progress = 50, step = "processing" })

    -- Simple data transformation
    local result = {
        id = input.id,
        status = "processed",
        message = "Successfully processed data: " .. (input.name or "unnamed"),
        timestamp = os.time()
    }

    -- Record heartbeat with completion
    activity.heartbeat({ progress = 100, step = "complete" })

    -- Check if canceled
    if activity.is_canceled() then
        print("Activity was canceled!")
        error("activity canceled")
    end

    return result
end

return { handler = handler }