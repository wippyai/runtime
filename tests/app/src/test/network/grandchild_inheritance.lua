-- SPDX-License-Identifier: MPL-2.0

-- Transitive overlay inheritance across a process chain. The test spawns a
-- mid-tier worker with {network = broken_socks5}; the mid-tier worker spawns
-- a grandchild with no options at all. The grandchild's http.get must still
-- route through the broken proxy, proving the overlay travels with the
-- context all the way down the tree — a child cannot bypass its ancestors'
-- overlay selection by simply not re-declaring it.

local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	local mid_pid, err = process.with_options({
		network = "app.test.network:broken_socks5",
	}):spawn_monitored("app.test.network:overlay_chain_worker", "app:processes")
	assert.is_nil(err, "spawn mid-tier: " .. tostring(err))
	assert.not_nil(mid_pid, "got mid-tier pid")

	local timeout = time.after("10s")
	local sel = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}
	if sel.channel == timeout then
		return false, "timeout waiting for mid-tier exit"
	end

	local event = sel.value
	assert.eq(event.kind, process.event.EXIT, "EXIT event")
	assert.eq(event.from, mid_pid, "EXIT from mid-tier")
	assert.is_nil(event.result.error, "mid-tier clean exit: " .. tostring(event.result.error))

	local payload = event.result.value
	assert.not_nil(payload, "mid-tier returned payload")
	assert.eq(payload.ok, false, "grandchild must not reach clearnet through inherited overlay")
	assert.not_nil(payload.err, "grandchild surfaced proxy error")

	return true
end

return { main = main }
