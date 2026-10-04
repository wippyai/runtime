-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main()
	time.sleep("10s")
	return "slept"
end

return { main = main }
