local time = require("time")

local function main(input)
	local name = "World"
	if input ~= nil and input.name ~= nil then
		name = input.name
	end

	time.sleep(100 * time.MILLISECOND)

	return {
		message = string.format("Hello, %s!", name),
		status = "completed",
		timestamp = time.now()
	}
end

return main
