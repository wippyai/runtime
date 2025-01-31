-- Execute workflow and get instance
local wf = temporal.client("default.temporal_client").execute("demo_workflow", {
    task_queue = "demo_queue"
})

-- Core instance interface
wf:signal("counter", 1)          -- send signal

local response = wf:response() -- get workflow completion channel

-- Example usage with timeout
local result = channel.select({
    response:case_receive(),
    time.after("10m"):case_receive()
})

if result.channel == response then
    if result.ok then
        -- handle workflow result
        print(result.value)
    else
        -- channel closed, check error
        print(wf:error())
    end
else
    -- timeout occurred
end