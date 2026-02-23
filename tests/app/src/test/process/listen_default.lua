-- SPDX-License-Identifier: MPL-2.0

-- Test: process.listen() default behavior returns raw payloads
-- This is the expected behavior for the gov client pattern

local assert = require("assert2")
local time = require("time")
local uuid = require("uuid")

local function main()
	_ = process.events()

	-- Create a channel with default settings (no options)
	local topic_name = "test.default." .. uuid.v4()
	local ch = process.listen(topic_name)
	assert.not_nil(ch, "created channel")

	-- Send ourselves a message with a table payload
	local ok = process.send(process.pid(), topic_name, {
		request_id = "test-123",
		data = "hello world",
		nested = { foo = "bar" }
	})
	assert.ok(ok, "send succeeded")

	-- Receive with timeout
	local timeout = time.after("1s")
	local result = channel.select({
		ch:case_receive(),
		timeout:case_receive()
	})

	assert.ok(result.channel ~= timeout, "received before timeout")
	assert.not_nil(result.value, "got value")

	local value = result.value

	-- Default behavior: value should be a raw Lua table, NOT a Message object
	assert.eq(type(value), "table", "value is a table")

	-- Should NOT have Message methods
	assert.is_nil(value.from, "no :from method (not a Message)")
	assert.is_nil(value.payload, "no :payload method (not a Message)")
	assert.is_nil(value.topic, "no :topic method (not a Message)")

	-- Should have direct field access
	assert.eq(value.request_id, "test-123", "direct access to request_id")
	assert.eq(value.data, "hello world", "direct access to data")
	assert.eq(type(value.nested), "table", "nested is a table")
	assert.eq(value.nested.foo, "bar", "direct access to nested.foo")

	return true
end

return { main = main }
