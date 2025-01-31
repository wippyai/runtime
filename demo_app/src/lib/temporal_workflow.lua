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

-- Command wrapper
local CommandHandle = {}
CommandHandle.__index = CommandHandle

function CommandHandle.new(cmd)
    local self = setmetatable({}, CommandHandle)
    self.cmd = cmd
    return self
end

function CommandHandle:await()
    -- Get both value and channel status
    local value, ok = self.cmd:response():receive()

    -- Check if channel was closed
    if not ok then
        -- Check if command failed
        local err = self.cmd:error()
        if err then
            error(err) -- Throw error if command failed
        end
        -- Return nil if channel was closed normally
        return nil
    end

    return value
end

function CommandHandle:response()
    return self.cmd:response()
end

function CommandHandle:error()
    return self.cmd:error()
end

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

    return function(...)
        local cmd = command.new(
            "activity",
            name,
            activity_config,
            ...
        )
        return CommandHandle.new(cmd)
    end
end

-- Create a timer command
function T.sleep(duration)
    return CommandHandle.new(command.new(
        "timer",
        { duration = duration }
    ))
end

-- Helper to find command by response channel
local function find_command_by_channel(commands, channel)
    for _, cmd in ipairs(commands) do
        if cmd:response() == channel then
            return cmd
        end
    end
    return nil
end

-- Helper to wait for first command to complete
function T.race(handles)
    if not handles or #handles == 0 then
        return nil
    end

    local cases = {}
    local commands = {}
    for _, handle in ipairs(handles) do
        table.insert(cases, handle:response():case_receive())
        table.insert(commands, handle)
    end

    local result = channel.select(cases)

    -- Find which command completed
    local completed = find_command_by_channel(commands, result.channel)
    if not completed then
        return nil
    end

    -- Check if channel was closed
    if not result.ok then
        local err = completed:error()
        if err then
            error(err)
        end
        return nil
    end

    return result.value
end

-- Helper to wait for all commands to complete
function T.parallel(handles)
    if not handles or #handles == 0 then
        return {}
    end

    local results = {}
    local remaining = #handles

    -- Track remaining commands and their indices
    local remaining_commands = {}
    for i, handle in ipairs(handles) do
        remaining_commands[handle:response()] = i
    end

    -- Wait for all commands to complete
    while remaining > 0 do
        local cases = {}
        for cmd, _ in pairs(remaining_commands) do
            table.insert(cases, cmd:case_receive())
        end

        local result = channel.select(cases)

        -- Get index from remaining_commands using the channel
        local idx = remaining_commands[result.channel]
        if idx then
            -- Remove from remaining commands
            remaining_commands[result.channel] = nil

            -- Check if channel was closed
            if not result.ok then
                -- Get the original command handle
                local cmd = handles[idx]
                local err = cmd:error()
                if err then
                    error(err)
                end
                results[idx] = nil
            else
                results[idx] = result.value
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