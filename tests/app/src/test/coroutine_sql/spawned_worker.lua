-- SPDX-License-Identifier: MPL-2.0

-- Worker: SQL query inside coroutine with channel.select pattern

local sql = require("sql")
local time = require("time")

local function run(parent_pid: string)
	if not parent_pid then
		error("parent_pid required")
	end

	local db, err = sql.get("app.test.coroutine_sql:testdb")
	if err then
		process.send(parent_pid, "test.response", { error = "setup get_db: " .. err })
		return
	end

	db:execute("CREATE TABLE IF NOT EXISTS spawned_test (id INTEGER PRIMARY KEY, name TEXT)")
	db:execute("DELETE FROM spawned_test")
	db:execute("INSERT INTO spawned_test (name) VALUES ('spawned_item')")
	db:release()

	local ops_channel = channel.new(10)
	local result_channel = channel.new(1)
	local done_channel = channel.new(1)

	local inbox = process.inbox()
	local events = process.events()

	coroutine.spawn(function()
		local result = channel.select({
			ops_channel:case_receive()
		})

		if result.ok and result.value then
			local handler_db, handler_err = sql.get("app.test.coroutine_sql:testdb")
			if handler_err then
				result_channel:send({ error = "handler get_db: " .. handler_err })
				done_channel:send(true)
				return
			end

			local query = sql.builder.select("id", "name")
			:from("spawned_test")
			:limit(1)

			local executor = query:run_with(handler_db)
			local rows, query_err = executor:query()
			handler_db:release()

			if query_err then
				result_channel:send({ error = "query: " .. query_err })
			elseif rows and #rows > 0 then
				result_channel:send({ name = rows[1].name })
			else
				result_channel:send({ error = "no rows" })
			end
		end

		done_channel:send(true)
	end)

	ops_channel:send({ type = "test_op" })

	local timeout = time.after("3s")
	local main_result = channel.select({
		inbox:case_receive(),
		events:case_receive(),
		result_channel:case_receive(),
		done_channel:case_receive(),
		timeout:case_receive()
	})

	if main_result.channel == timeout then
		process.send(parent_pid, "test.response", { error = "timeout: worker SQL query did not complete" })
		return
	end

	if main_result.channel == result_channel then
		process.send(parent_pid, "test.response", main_result.value)
		return
	end

	if main_result.channel == done_channel then
		local res, ok = result_channel:receive()
		if ok then
			process.send(parent_pid, "test.response", res)
		else
			process.send(parent_pid, "test.response", { error = "done without result" })
		end
		return
	end

	if main_result.channel == events then
		local event = main_result.value
		if event.kind == process.event.CANCEL then
			process.send(parent_pid, "test.response", { error = "cancelled" })
		end
		return
	end

	process.send(parent_pid, "test.response", { error = "unexpected channel" })
end

return { run = run }
