-- Execute workflow and get instance
local wf = temporal.client("temporal_client)_name").execute(
    "demo_workflow",
    {
        task_queue = "demo_queue"
    },
    args...
)

wf:info().workflow_id -- id
wf:info().run_id -- run id

-- get exiting workflow
local wf = temporal.client("temporal_client)_name").get_workflow(wf:run_info().workflow_id)

-- Core instance interface
wf:signal("counter", 1)          -- send signal

-- channel!
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