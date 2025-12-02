local http = require("http")
local time = require("time")

local function handler()
    local res, res_err = http.response()
    local req, req_err = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

     -- Sleep for 10ms to test dispatcher
     --time.sleep("1ms")

        -- Call WASM add function (2 + 3 = 5)
       --local sum, err = funcs.new():call("app.api:add", 2, 3)
       -- --
       -- local data = {
       --     message = "hello world",
       --     slept = "10ms",
       --     wasm_add = sum,
       --     wasm_err = err
       -- }

    ---- Spawn a monitored worker process
    --local worker_pid, spawn_err = process.spawn("app.api:worker", "app:processes", "hello from handler")
    --
    local data = {
        message = "hello world",
    --    worker_pid = worker_pid,
    --    spawn_error = spawn_err
    }

    --if worker_pid then
    --    print("[HANDLER] Spawned monitored worker: " .. worker_pid)
    --end
    --if spawn_err then
    --    print("[HANDLER] Spawn error: " .. spawn_err)
    --end

    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json(data)
end

return {
    handler = handler
}
