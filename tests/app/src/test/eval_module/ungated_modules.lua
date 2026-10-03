-- SPDX-License-Identifier: MPL-2.0

-- Test: modules that act on host-wide state without a permission check of
-- their own still obey an explicit deny, which an eval's policy issues for
-- everything it does not allow.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()
	local child, err = eval.spawn([[
		return { main = function()
			local ok, metric_err = metrics.counter_inc("eval_probe_total", { origin = "eval" })
			return { ok = ok, err = tostring(metric_err) }
		end }
	]], { modules = { "metrics" }, monitor_only = true })
	assert.is_nil(err, "spawn succeeds")

	local timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == child then
			local result = selected.value.result
			assert.is_nil(result.error, "eval process succeeds")
			assert.is_nil(result.value.ok, "metrics are refused")
			assert.contains(result.value.err, "not allowed", "the eval policy denies metrics")
			return true
		end
	end
end

return { main = main }
