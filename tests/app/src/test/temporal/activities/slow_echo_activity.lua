local time = require("time")

local function main(input)
	input = input or {}

	time.sleep(input.delay or "1s")

	return {
		message = input.message or "slow",
	}
end

return main
