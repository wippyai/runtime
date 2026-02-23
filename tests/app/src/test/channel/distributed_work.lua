-- SPDX-License-Identifier: MPL-2.0

-- Test: Distributed work simulation with time.sleep
-- Simulates async workers processing jobs with varying delays
local assert = require("assert2")
local time = require("time")

local function main()
-- Test 1: Multiple workers with time delays
	local work_queue = channel.new(10)
	local results = channel.new(10)
	local worker_count = 3
	local job_count = 6

	-- Spawn workers that simulate processing time
	for w = 1, worker_count do
		coroutine.spawn(function()
			while true do
				local job, ok = work_queue:receive()
				if not ok then
					break
				end
				-- Simulate processing delay (10ms per job)
				time.sleep(10 * time.MILLISECOND)
				results:send({worker = w, job = job, result = job * 2})
			end
		end)
	end

	-- Producer sends jobs
	for i = 1, job_count do
		work_queue:send(i)
	end
	work_queue:close()

	-- Collect results
	local total = 0
	for _ = 1, job_count do
		local r = results:receive()
		total = total + r.result
	end

	-- sum of (1+2+3+4+5+6)*2 = 42
	assert.eq(total, 42, "all jobs processed correctly")

	-- Test 2: Staggered producer with select and timeout simulation
	local fast_ch = channel.new(1)
	local slow_ch = channel.new(1)
	coroutine.spawn(function()
		time.sleep(5 * time.MILLISECOND)
		fast_ch:send("fast")
	end)

	coroutine.spawn(function()
		time.sleep(20 * time.MILLISECOND)
		slow_ch:send("slow")
	end)

	-- First select should get fast channel
	local result1 = channel.select{
		fast_ch:case_receive(),
		slow_ch:case_receive()
	}
	assert.eq(result1.value, "fast", "fast channel arrived first")

	-- Second select should get slow channel
	local result2 = channel.select{
		fast_ch:case_receive(),
		slow_ch:case_receive()
	}
	assert.eq(result2.value, "slow", "slow channel arrived second")

	-- Test 3: Pipeline with processing stages
	local stage1 = channel.new(5)
	local stage2 = channel.new(5)
	local stage3 = channel.new(5)

	-- Stage 1: Input processing
	coroutine.spawn(function()
		for i = 1, 3 do
			time.sleep(5 * time.MILLISECOND)
			stage1:send(i * 10)
		end
		stage1:close()
	end)

	-- Stage 2: Transform
	coroutine.spawn(function()
		while true do
			local v, ok = stage1:receive()
			if not ok then
				break
			end
			time.sleep(5 * time.MILLISECOND)
			stage2:send(v + 1)
		end
		stage2:close()
	end)

	-- Stage 3: Aggregate
	coroutine.spawn(function()
		local sum = 0
		while true do
			local v, ok = stage2:receive()
			if not ok then
				break
			end
			time.sleep(5 * time.MILLISECOND)
			sum = sum + v
		end
		stage3:send(sum)
	end)

	local final = stage3:receive()
	-- (10+1) + (20+1) + (30+1) = 11 + 21 + 31 = 63
	assert.eq(final, 63, "pipeline processed correctly")

	return true
end

return { main = main }
