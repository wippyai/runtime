-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main(input)
	local worker_count = 3
	local job_count = 6

	if input ~= nil then
		worker_count = input.workers or worker_count
		job_count = input.jobs or job_count
	end

	local work_queue = channel.new(10)
	local results = channel.new(10)

	-- Spawn workers that process jobs with simulated delay
	for w = 1, worker_count do
		coroutine.spawn(function()
			while true do
				local job, ok = work_queue:receive()
				if not ok then
					break
				end
				time.sleep(10 * time.MILLISECOND)
				results:send({worker = w, job = job, result = job * 2})
			end
		end)
	end

	-- Send jobs
	for j = 1, job_count do
		work_queue:send(j)
	end
	work_queue:close()

	-- Collect results
	local total = 0
	local processed = {}
	for _ = 1, job_count do
		local r = results:receive()
		total = total + r.result
		table.insert(processed, r)
	end

	return {
		total = total,
		job_count = job_count,
		worker_count = worker_count,
		processed = processed
	}
end

return main
