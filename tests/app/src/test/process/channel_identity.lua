-- Test: Channel identity stability
-- Verifies that channels from inbox(), events(), listen() maintain identity
-- across multiple accesses and in channel.select results
local time = require("time")

local function main()
-- ============================================
-- PART 1: Basic identity - multiple calls
-- ============================================

-- Test 1.1: inbox() returns same channel on multiple calls
	local inbox1 = process.inbox()
	local inbox2 = process.inbox()
	local inbox3 = process.inbox()
	if inbox1 ~= inbox2 then
		return false, "inbox(): call 1 ~= call 2"
	end
	if inbox2 ~= inbox3 then
		return false, "inbox(): call 2 ~= call 3"
	end

	-- Test 1.2: events() returns same channel on multiple calls
	local events1 = process.events()
	local events2 = process.events()
	local events3 = process.events()
	if events1 ~= events2 then
		return false, "events(): call 1 ~= call 2"
	end
	if events2 ~= events3 then
		return false, "events(): call 2 ~= call 3"
	end

	-- Test 1.3: listen() returns same channel for same topic
	local listen1 = process.listen("test_topic_a")
	local listen2 = process.listen("test_topic_a")
	local listen3 = process.listen("test_topic_a")
	if listen1 ~= listen2 then
		return false, "listen(topic_a): call 1 ~= call 2"
	end
	if listen2 ~= listen3 then
		return false, "listen(topic_a): call 2 ~= call 3"
	end

	-- Test 1.4: listen() returns different channels for different topics
	local listen_b = process.listen("test_topic_b")
	if listen1 == listen_b then
		return false, "listen(topic_a) == listen(topic_b) - should be different"
	end

	-- Test 1.5: inbox, events, listen are all different from each other
	if inbox1 == events1 then
		return false, "inbox() == events() - should be different"
	end
	if inbox1 == listen1 then
		return false, "inbox() == listen() - should be different"
	end
	if events1 == listen1 then
		return false, "events() == listen() - should be different"
	end

	-- ============================================
	-- PART 2: Identity after interleaved calls
	-- ============================================

	-- Test 2.1: Get all three, then get them again
	local inbox_a = process.inbox()
	local events_a = process.events()
	local listen_a = process.listen("interleave_topic")

	local inbox_b = process.inbox()
	local events_b = process.events()
	local listen_b2 = process.listen("interleave_topic")

	if inbox_a ~= inbox_b then
		return false, "inbox identity lost after interleaved calls"
	end
	if events_a ~= events_b then
		return false, "events identity lost after interleaved calls"
	end
	if listen_a ~= listen_b2 then
		return false, "listen identity lost after interleaved calls"
	end

	-- ============================================
	-- PART 3: Identity in select results
	-- ============================================

	local events_ch = process.events()

	-- Spawn a monitored worker that exits immediately
	local _, err = process.spawn_monitored("app.test.process:instant_exit_worker", "app:processes")
	if err then
		return false, "spawn failed: " .. tostring(err)
	end

	-- Wait for EXIT event
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for EXIT event"
	end

	-- Test 3.1: result.channel must be identical to events_ch
	if result.channel ~= events_ch then
		return false, "select result.channel ~= events_ch"
	end

	-- Test 3.2: events() still returns same channel after select
	local events_after_select = process.events()
	if events_after_select ~= events_ch then
		return false, "events() changed after select"
	end

	-- ============================================
	-- PART 4: Identity with inbox in select
	-- ============================================

	local inbox_ch = process.inbox()

	-- Spawn worker that sends us a message
	_, err = process.spawn_monitored("app.test.process:send_and_exit_worker", "app:processes", process.pid())
	if err then
		return false, "spawn send worker failed: " .. tostring(err)
	end

	-- Select on inbox
	timeout = time.after("2s")
	result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for inbox message"
	end

	-- Test 4.1: result.channel must be identical to inbox_ch
	if result.channel ~= inbox_ch then
		return false, "select result.channel ~= inbox_ch"
	end

	-- Test 4.2: inbox() still returns same channel after select
	local inbox_after_select = process.inbox()
	if inbox_after_select ~= inbox_ch then
		return false, "inbox() changed after select"
	end

	-- Wait for EXIT event
	timeout = time.after("2s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for EXIT after inbox"
	end

	-- ============================================
	-- PART 5: Identity with listen in select
	-- ============================================

	local custom_ch = process.listen("custom_select_topic")

	-- Spawn worker that sends to custom topic
	_, err = process.spawn_monitored("app.test.process:send_custom_worker", "app:processes", process.pid(), "custom_select_topic")
	if err then
		return false, "spawn custom worker failed: " .. tostring(err)
	end

	-- Select on custom topic
	timeout = time.after("2s")
	result = channel.select {
		custom_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for custom topic message"
	end

	-- Test 5.1: result.channel must be identical to custom_ch
	if result.channel ~= custom_ch then
		return false, "select result.channel ~= custom_ch (listen)"
	end

	-- Test 5.2: listen() still returns same channel after select
	local custom_after_select = process.listen("custom_select_topic")
	if custom_after_select ~= custom_ch then
		return false, "listen() changed after select"
	end

	-- Wait for EXIT
	timeout = time.after("2s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for worker EXIT"
	end

	-- ============================================
	-- PART 6: Combined select with all three
	-- ============================================

	-- Fresh references
	local final_inbox = process.inbox()
	local final_events = process.events()
	local final_listen = process.listen("final_topic")

	-- Verify they match earlier references
	if final_inbox ~= inbox_ch then
		return false, "final inbox ~= earlier inbox"
	end
	if final_events ~= events_ch then
		return false, "final events ~= earlier events"
	end

	-- Spawn worker for final test
	_, err = process.spawn_monitored("app.test.process:instant_exit_worker", "app:processes")
	if err then
		return false, "spawn final worker failed: " .. tostring(err)
	end

	-- Select on all three plus timeout
	timeout = time.after("2s")
	result = channel.select {
		final_inbox:case_receive(),
		final_events:case_receive(),
		final_listen:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout in combined select"
	end

	-- Test 6.1: Must match one of the stored channels
	local matched = false
	if result.channel == final_inbox then
		matched = true
	end
	if result.channel == final_events then
		matched = true
	end
	if result.channel == final_listen then
		matched = true
	end

	if not matched then
		return false, "combined select: result.channel matches none of the stored channels"
	end

	return true
end

return { main = main }
