-- SPDX-License-Identifier: MPL-2.0

-- Child that ignores every event: it traps links and never leaves its loop.
local function main()
	process.set_options({ trap_links = true })
	local events = process.events()
	while true do
		events:receive()
	end
end

return { main = main }
