local wf = require "temporal_workflow"

local activities = wf.init_activities({
    hello_world = {
        name = "hello_world.activity",
        config = {
            task_queue = "wippy_demos",
            schedule_to_close_timeout = "10s"
        }
    },
})

function execute_workflow()
    wf.sleep("5s")

    local first = wf.race({
        activities.hello_world("Hello", "World"),
        wf.sleep("5s")
    })

    return first
end
