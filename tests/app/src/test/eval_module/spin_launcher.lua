-- SPDX-License-Identifier: MPL-2.0

-- Runs as an unowned process and detaches an eval that spins under the
-- largest step limits, so only its detached lifetime can stop it.
local function main()
	local pid, err = eval.spawn([[
		return { main = function() while true do end end }
	]], {
		detached = true,
		allow_detached = true,
		limits = { tick_budget = 65536, max_steps = 1048576 },
	})
	if not pid then error(err) end
	return pid
end

return { main = main }
