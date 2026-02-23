-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main(input)
	local steps = input and input.steps or 3
	local delay_ms = input and input.delay_ms or 100

	local timeline = {}
	local start = time.now()

	table.insert(timeline, {
		step = 0,
		action = "start",
		elapsed_ms = 0
	})

	for i = 1, steps do
		time.sleep(delay_ms * time.MILLISECOND)
		local elapsed = time.now():sub(start):milliseconds()
		table.insert(timeline, {
			step = i,
			action = "after_sleep",
			elapsed_ms = math.floor(elapsed)
		})
	end

	local total_elapsed = time.now():sub(start):milliseconds()

	return {
		steps = steps,
		delay_ms = delay_ms,
		total_elapsed_ms = math.floor(total_elapsed),
		timeline = timeline
	}
end

return main
