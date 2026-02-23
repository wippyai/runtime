-- SPDX-License-Identifier: MPL-2.0

-- Test: Security context inheritance through multi-level spawn
-- This test validates that actor/scope is inherited when:
-- 1. Test spawns Child WITH process.with_context().with_actor().with_scope()
-- 2. Child spawns Grandchild with plain process.spawn_monitored() (NO with_context)
-- 3. Grandchild should still have actor/scope from the original context
local assert = require("assert2")
local security = require("security")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Create actor and scope for the test
	local actor = security.new_actor("inherit_test_user", {
		role = "tester"
	})
	assert.not_nil(actor, "actor should be created")

	local scope = security.new_scope()
	assert.not_nil(scope, "scope should be created")

	-- Spawn the parent worker WITH explicit actor/scope
	-- This parent will then spawn a grandchild WITHOUT with_context
	local child_pid, err = process.with_context({})
	:with_actor(actor)
	:with_scope(scope)
	:spawn_monitored("app.test.process:ctx_inherit_worker", "app:processes")

	assert.is_nil(err, "spawn with context should not error")
	assert.not_nil(child_pid, "spawn should return pid")

	-- Wait for child (which waits for grandchild) to exit
	local timeout = time.after("5s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for child exit"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, child_pid, "event from child")

	-- If there's an error, the grandchild didn't inherit security context
	if event.error then
		return false, "inheritance failed: " .. tostring(event.error)
	end

	return true
end

return { main = main }
