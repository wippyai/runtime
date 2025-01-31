local temporal = require("temporal")
local httpctx = require("httpctx")

function handle()
    local res = httpctx.response()

    -- Try to get default client
    local client, err = temporal.client("temporal")
    if err then
        res:set_status(500)
        res:write(err .. "\n")
        res:flush()
        return
    end

    client:healthcheck()

    res:set_status(200)
    res:write("hello \n")
    res:flush()

    -- Get a list of current workflows (this is just an example)
    --local result = {
    --    client_connected = true,
    --    test_workflow = {
    --        workflow_id = "test_workflow_" .. tostring(os.time())
    --    }
    --}
    --
    ---- Try to execute a test workflow
    --local execution, err = client:execute("demo_workflow", {
    --    task_queue = "demo_queue"
    --})
    --
    --if err then
    --    result.test_workflow.error = err.message
    --else
    --    result.test_workflow.status = "started"
    --    result.test_workflow.run_id = execution.run_id
    --end

    --return json.encode(result)
end
