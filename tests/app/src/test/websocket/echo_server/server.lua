-- WebSocket Echo Server Process
-- Listens for WebSocket messages and echoes them back to the client
local time = require("time")
local json = require("json")

local function main()
-- Subscribe to WebSocket topics with message mode to get :topic() and :payload()
	local msg_ch = process.listen("ws.message", { message = true })
	local join_ch = process.listen("ws.join", { message = true })
	local leave_ch = process.listen("ws.leave", { message = true })

	-- Track connected client
	local client_pid = nil

	-- Main message loop
	while true do
		local timeout = time.after("60s")
		local result = channel.select {
			msg_ch:case_receive(),
			join_ch:case_receive(),
			leave_ch:case_receive(),
			timeout:case_receive(),
		}

		if result.channel == timeout or not result.ok then
			break
		end

		local msg = result.value
		local topic = msg:topic()
		local payload = msg:payload():data()

		if topic == "ws.join" then
		-- Client connected - payload is JSON with client_pid
			if type(payload) == "table" then
				client_pid = payload.client_pid
			elseif type(payload) == "string" then
				local decoded = json.decode(payload)
				if decoded and decoded.client_pid then
					client_pid = decoded.client_pid
				end
			end

		elseif topic == "ws.leave" then
		-- Client disconnected
			client_pid = nil
			break

		elseif topic == "ws.message" then
		-- Echo message back to client
			if type(client_pid) == "string" then
				process.send(client_pid, "ws.message", payload)
			end
		end
	end

	return true
end

return { main = main }
