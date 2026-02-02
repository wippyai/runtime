-- WebSocket Echo Endpoint
-- HTTP handler that upgrades to WebSocket via wsrelay middleware

local function handler()
	local http = require("http")
	local json = require("json")
	local process = require("process")

	local res = http.response()
	local req = http.request()

	if not res or not req then
		return nil, "Failed to get HTTP context"
	end

	-- Spawn the echo server process
	local pid, err = process.spawn("app.test.websocket.echo_server:server", "app:processes")
	if err then
		res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
		res:write_json({ error = "Failed to spawn echo server: " .. err })
		return
	end

	-- Set the X-WS-Relay header to trigger WebSocket upgrade
	local relay_config = json.encode({
		target_pid = pid,
		message_topic = "ws.message"
	})

	res:set_header("X-WS-Relay", relay_config)
end

return { handler = handler }
