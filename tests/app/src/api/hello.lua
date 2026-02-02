local http = require("http")

local function handler()
	local res, _ = http.response()
	local req, _ = http.request()

	if not res or not req then
		return nil, "Failed to get HTTP context"
	end

	-- Sleep for 1ms
	--time.sleep("5ms")
	--
	local data = {
		message = "hello world"
	}

	---- Spawn a monitored worker process
	--local worker_pid, spawn_err = process.spawn("app.api:worker", "app:processes", "hello from handler")
	--
	--local data = {
	--    message = "hello world",
	--  --  worker_pid = worker_pid,
	--  --  spawn_error = spawn_err
	--}

	--if worker_pid then
	--    print("[HANDLER] Spawned monitored worker: " .. worker_pid)
	--end
	--if spawn_err then
	--    print("[HANDLER] Spawn error: " .. spawn_err)
	--end

	res:set_content_type(http.CONTENT.JSON)
	res:set_status(http.STATUS.OK)
	res:write_json(data)
	return nil
end

return {
	handler = handler
}
