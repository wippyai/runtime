local temporal = require("temporal")
local httpctx = require("httpctx")
local time = require("time")

function handle()
    local res = httpctx.response()

    -- Get client
    local client, err = temporal.client("temporal")
    if err then
        res:set_status(500)
        res:write("Failed to get client: " .. err .. "\n")
        res:flush()
        return
    end

    -- Start workflow
    local wf, err = client:execute("demo_workflow", {
        task_queue = "wippy_demos",
        workflow_run_timeout = "10m"
    })

    if err then
        res:set_status(500)
        res:write("Failed to start workflow: " .. err .. "\n")
        res:flush()
        return
    end

    -- Get workflow info
    local info = wf:info()
    res:write("Started workflow: " .. info.workflow_id .. "\n")
    res:flush()

    -- Send 10 signals
    for i = 1, 10 do
        res:write("Sending signal " .. i .. "...\n")
        res:flush()

        local ok, err = wf:signal("signal", "Signal " .. i)
        if err then
            res:write("Failed to send signal " .. i .. ": " .. err .. "\n")
            res:flush()
        else
            res:write("Sent signal " .. i .. "\n")
            res:flush()
        end
        res:flush()
        time.sleep("100ms")
    end

    -- Wait for completion using response channel
    local ch = wf:response()
    local result = channel.select({
        ch:case_receive(),
        time.after("1m"):case_receive()
    })

    if result.channel == ch then
        if result.ok then
            res:write("Workflow completed successfully: \n")
        else
            res:write("Workflow failed: " .. wf:error() .. "\n")
        end
    else
        res:write("Workflow timed out waiting for completion\n")
    end

    res:flush()
end
