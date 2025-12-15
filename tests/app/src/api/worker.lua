-- Simple worker process
local process = require("process")

local function main(input)
    -- Get our PID
    local pid = process.pid()
   -- print("[WORKER] Started with PID: " .. pid)

    -- Process input if provided
    if input then
     --   print("[WORKER] Received input: " .. tostring(input))
    end

    -- Do some work
   -- print("[WORKER] Processing...")

    --print("[WORKER] Completed")
    return "done"
end

return {
    main = main
}
