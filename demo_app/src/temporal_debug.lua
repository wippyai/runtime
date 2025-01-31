local temporal = require("temporal")
local json = require("json")
local httpctx = require("httpctx")

function handle()
    local res = httpctx.response()

    -- Try to get default client
    local client, err = temporal.client("default.temporal_client")
    if err then
        res.status = 500
        return json.encode({
            error = true,
            message = err.message
        })
    end

    -- Get a list of current workflows (this is just an example)
    local result = {
        client_connected = true,
        test_workflow = {
            workflow_id = "test_workflow_" .. tostring(os.time())
        }
    }

    -- Try to execute a test workflow
    local execution, err = client:execute("demo_workflow", {
        task_queue = "demo_queue"
    })

    if err then
        result.test_workflow.error = err.message
    else
        result.test_workflow.status = "started"
        result.test_workflow.run_id = execution.run_id
    end

    return json.encode(result)
end
