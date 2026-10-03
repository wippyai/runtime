-- SPDX-License-Identifier: MPL-2.0

-- Spawns sleepers until the runtime refuses, then returns how many started
-- and the refusal.
local function main()
	local started = 0
	local refusal = ""
	for _ = 1, 3 do
		local pid, err = process.spawn("app.test.eval_module:long_sleeper", "app:processes")
		if not pid then
			refusal = tostring(err)
			break
		end
		started = started + 1
	end
	return { started = started, refusal = refusal }
end

return { main = main }
