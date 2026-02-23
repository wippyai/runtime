-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local system = require("system")

	-- Test process.pid
	local pid, err = system.process.pid()
	assert.is_nil(err, "pid should not error")
	assert.not_nil(pid, "pid returned")
	assert.eq(type(pid), "number", "pid is number")
	assert.ok(pid > 0, "pid > 0")

	-- Test process.hostname
	local name, err = system.process.hostname()
	assert.is_nil(err, "hostname should not error")
	assert.not_nil(name, "hostname returned")
	assert.eq(type(name), "string", "hostname is string")
	assert.ok(#name > 0, "hostname not empty")

	return true
end

return { main = main }
