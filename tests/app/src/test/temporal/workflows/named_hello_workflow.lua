-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main(input)
	local name = "World"
	if input ~= nil and input.name ~= nil then
		name = input.name
	end

	time.sleep(50 * time.MILLISECOND)

	return {
		message = string.format("Named hello, %s!", name),
		workflow = "ExternalNamedWorkflow",
		status = "completed",
		timestamp = time.now()
	}
end

return main
