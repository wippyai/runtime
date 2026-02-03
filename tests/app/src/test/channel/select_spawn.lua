-- Test: Select with coroutine.spawn
local assert = require("assert2")

local function main()
-- Test select blocks until channel ready
	local ch1 = channel.new(0)
	local ch2 = channel.new(0)
	local result_ch = channel.new(1)

	-- Spawn a select that blocks on both channels
	coroutine.spawn(function()
		local result = channel.select{
			ch1:case_receive(),
			ch2:case_receive()
		}
		-- Send which channel was selected (1 or 2) and the value
		local which = 0
		if result.channel == ch1 then
			which = 1
		end
		if result.channel == ch2 then
			which = 2
		end
		result_ch:send({
			which = which,
			value = result.value,
			ok = result.ok
		})
	end)

	-- Send to ch2 to wake the select
	ch2:send("wakeup")

	local r = result_ch:receive()
	assert.eq(r.which, 2, "select woke on ch2")
	assert.eq(r.value, "wakeup", "got correct value")
	assert.eq(r.ok, true, "ok is true")

	-- Test select wakes on close
	local ch3 = channel.new(0)
	local ch4 = channel.new(0)
	local result_ch2 = channel.new(1)

	coroutine.spawn(function()
		local result = channel.select{
			ch3:case_receive(),
			ch4:case_receive()
		}
		local which = 0
		if result.channel == ch3 then
			which = 3
		end
		if result.channel == ch4 then
			which = 4
		end
		result_ch2:send({
			which = which,
			ok = result.ok
		})
	end)

	-- Close ch3 to wake the select
	ch3:close()

	local r2 = result_ch2:receive()
	assert.eq(r2.which, 3, "select woke on closed ch3")
	assert.eq(r2.ok, false, "ok is false for closed")

	-- Test multi-channel select with case_send
	local send_ch = channel.new(1)
	local recv_ch = channel.new(1)
	recv_ch:send("existing")

	local result3 = channel.select{
		send_ch:case_send("outgoing"),
		recv_ch:case_receive()
	}

	-- Both could succeed, just verify we got a valid result
	assert.eq(result3.ok, true, "select succeeded")

	return true
end

return { main = main }
