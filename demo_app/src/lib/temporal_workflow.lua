local T = {}

-- Default configurations
local DEFAULT_CONFIG = {
    task_queue = "default",
    schedule_to_close_timeout = "30s",
    retry_policy = {
        initial_interval = "1s",
        backoff_coefficient = 2.0,
        maximum_interval = "100s",
        maximum_attempts = 3
    }
}

-- Helper to merge tables
local function merge(t1, t2)
    if not t2 then return t1 end
    local result = {}
    for k, v in pairs(t1) do result[k] = v end
    for k, v in pairs(t2) do result[k] = v end
    return result
end

-- Create activity definition that can be called multiple times
function T.activity(name, config)
    local activity_config = merge(DEFAULT_CONFIG, config)

    -- Return callable function that creates new command each time
    return function(...)
        local cmd = command.new(
            "activity",
            name,
            activity_config,
            ...
        )
        return cmd
    end
end

-- Create a timer command
function T.sleep(duration)
    return command.new(
        "timer",
        { duration = duration }
    )
end

-- Helper to wait for command with timeout
function T.with_timeout(cmd, timeout)
    local timer = T.sleep(timeout)

    local result = channel.select{
        cmd:response():case_receive(),
        timer:response():case_receive()
    }

    -- If timer case was selected, activity didn't complete in time
    if result.case == timer:response():case_receive() then
        return nil, false
    end

    -- Activity completed in time
    if not result.ok then
        error("Activity failed")
    end

    return result.value, true
end

-- Helper to wait for first command to complete
function T.race(commands)
    local cases = {}
    for _, cmd in ipairs(commands) do
        table.insert(cases, cmd:response():case_receive())
    end

    local result = channel.select(cases)
    if not result.ok then
        error("Activity failed in race")
    end
    return result.value
end

-- Helper to wait for all commands to complete
function T.parallel(commands)
    local results = {}
    local remaining = #commands

    -- Create receiving channels array
    local cases = {}
    local cmd_map = {} -- Map cases back to command indices

    for i, cmd in ipairs(commands) do
        local case = cmd:response():case_receive()
        table.insert(cases, case)
        cmd_map[case] = i
    end

    -- Wait for all commands to complete
    while remaining > 0 do
        local result = channel.select(cases)

        -- Find which command completed
        local cmd_idx = cmd_map[result.case]
        if cmd_idx then
            if not result.ok then
                error("Activity failed in parallel execution")
            end

            -- Store result and remove the case
            results[cmd_idx] = result.value
            for i, case in ipairs(cases) do
                if case == result.case then
                    table.remove(cases, i)
                    break
                end
            end

            remaining = remaining - 1
        end
    end

    return results
end

-- Create a workflow scope for defining multiple activities
function T.init_activities(activities_def)
    local activities = {}

    for name, def in pairs(activities_def) do
        if type(def) == "table" then
            activities[name] = T.activity(def.name, def.config)
        else
            activities[name] = T.activity(def)
        end
    end

    return activities
end

return T