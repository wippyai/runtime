-- SPDX-License-Identifier: MPL-2.0

-- Test: Coroutine in select loop receives buffered channel message after main blocks

local assert = require("assert2")
local sql = require("sql")
local time = require("time")

local function main()
	local db, err = sql.get("app.test.coroutine_sql:testdb")
	assert.is_nil(err, "should get database")
	db:execute("CREATE TABLE IF NOT EXISTS bus_test (id INTEGER PRIMARY KEY, val TEXT)")
	db:execute("DELETE FROM bus_test")
	db:execute("INSERT INTO bus_test (val) VALUES ('test')")
	db:release()

	local ops_channel = channel.new(256)
	local stop_signal = channel.new(0)
	local bus_done = channel.new(0)
	local result_channel = channel.new(1)

	coroutine.spawn(function()
		while true do
			local result = channel.select({
				stop_signal:case_receive(),
				ops_channel:case_receive()
			})

			if result.channel == stop_signal then
				bus_done:send(true)
				return
			end

			if result.channel == ops_channel then
				local coro_db, coro_err = sql.get("app.test.coroutine_sql:testdb")
				if coro_err then
					result_channel:send({error = "db error: " .. tostring(coro_err)})
					return
				end

				local rows, query_err = coro_db:query("SELECT * FROM bus_test LIMIT 1")
				coro_db:release()

				if query_err then
					result_channel:send({error = "query error: " .. tostring(query_err)})
					return
				end

				result_channel:send({success = true, data = rows[1]})
				stop_signal:send(true)
			end
		end
	end)

	time.sleep(0.01)

	ops_channel:send({type = "test_op"})

	local timeout = time.after("2s")
	local final = channel.select({
		result_channel:case_receive(),
		bus_done:case_receive(),
		timeout:case_receive()
	})

	if final.channel == timeout then
		error("timeout: coroutine did not receive buffered message")
	end

	if final.channel == result_channel then
		local res = final.value
		if res.error then
			error("coroutine error: " .. res.error)
		end
		assert.not_nil(res.data, "should have result data")
	end

	return true
end

return { main = main }
