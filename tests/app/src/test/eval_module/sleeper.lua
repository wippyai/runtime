-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main()
	time.sleep("100ms")
	return "slept"
end

return { main = main }
