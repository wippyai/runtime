-- Long-running workflow for cancellation testing
local time = require("time")

local function main(input)
	local iterations = 0
	local max_iterations = input and input.iterations or 100

	while iterations < max_iterations do
		time.sleep(100 * time.MILLISECOND)
		iterations = iterations + 1
	end

	return {
		iterations = iterations,
		status = "completed"
	}
end

return main
