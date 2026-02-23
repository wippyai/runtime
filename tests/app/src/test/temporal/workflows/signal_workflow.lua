-- SPDX-License-Identifier: MPL-2.0

local funcs = require("funcs")

local function main(input)
	local results = {}

	-- Listen for signals on topics
	local job_ch = process.listen("add_job")
	local exit_ch = process.listen("exit")

	-- Main loop - blocks waiting for signals
	while true do
		local result = channel.select{
			job_ch:case_receive(),
			exit_ch:case_receive()
		}

		if result.channel == exit_ch then
		-- Exit signal received, break loop
			break
		elseif result.channel == job_ch then
		-- Job signal received, call activity
			local job_data = result.value
			local activity_result, err = funcs.call("app.test.temporal.activities:echo_activity", {
				job_id = job_data and job_data.id or "unknown",
				data = job_data
			})

			if err then
				table.insert(results, {
					job_id = job_data and job_data.id or "unknown",
					error = tostring(err)
				})
			else
				table.insert(results, {
					job_id = job_data and job_data.id or "unknown",
					result = activity_result
				})
			end
		end
	end

	return {
		total_jobs = #results,
		results = results,
		input = input
	}
end

return main
