-- SPDX-License-Identifier: MPL-2.0

-- Process that sleeps before returning
local time = require("time")

local function main(delay)
	delay = delay or "100ms"
	time.sleep(delay)
	return { completed = true, delay = delay }
end

return { main = main }
