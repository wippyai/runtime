-- Test: SQL query inside coroutine in spawned process completes correctly

local assert = require("assert2")
local time = require("time")

local function main()
	local my_pid = process.pid()
	assert.not_nil(my_pid, "should have own pid")

	local worker_pid, spawn_err = process.spawn(
		"app.test.coroutine_sql:spawned_worker",
		"app:processes",
		my_pid
	)
	assert.is_nil(spawn_err, "should spawn worker")
	assert.not_nil(worker_pid, "should have worker pid")

	local response_ch = process.listen("test.response")
	local timeout = time.after("3s")

	local result = channel.select({
		response_ch:case_receive(),
		timeout:case_receive()
	})

	if result.channel == timeout then
		error("timeout: spawned process SQL query did not complete")
	end

	local response = result.value
	if response.error then
		error("worker error: " .. response.error)
	end

	assert.eq(response.name, "spawned_item", "should get correct value from spawned process")
	return true
end

return { main = main }
