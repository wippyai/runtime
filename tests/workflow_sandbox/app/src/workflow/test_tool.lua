local workflow_sandbox = require("workflow_sandbox")

function main()
    local wf, err = workflow_sandbox.get("app.workflows:simple_workflow")
    if err then
        error("failed to get workflow: " .. tostring(err))
    end

    print("Starting workflow...")

    local ok, err = wf:step("main", "test input")
    if not ok then
        error("step failed: " .. tostring(err))
    end

    local commands = wf:commands()
    print("Got " .. #commands .. " commands")

    for i, cmd in ipairs(commands) do
        print("Command " .. i .. ": " .. cmd:type())

        cmd:complete(true)
    end

    local ok, err = wf:step()
    if not ok then
        error("step 2 failed: " .. tostring(err))
    end

    if wf:done() then
        print("Workflow completed!")
        local result, err = wf:result()
        if err then
            error("error getting result: " .. tostring(err))
        end
        print("Result: " .. tostring(result))
    else
        print("Workflow not done yet")
    end

    wf:close()

    return "test completed"
end
