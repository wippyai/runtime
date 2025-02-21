local time = require("time")
local json = require("json")

local function run(args)
    if not args then
        error("No arguments provided")
    end

    --print(string.format("Child process started: %s with args: %s",
    --    process.pid(),
    --    json.encode(args)))

    -- Simulate work
    --time.sleep("500ms")

    -- Send completion message
    --process.send(args.parent_pid, "child_msgs", {
    --    from = process.pid(),
    --    child_number = args.child_number,
    --    status = "completed",
    --    timestamp = time.now():format("15:04:05")
    --})

    return {
        name = args.name,
        status = "completed",
        child_number = args.child_number
    }
end

return {
    run = run
}