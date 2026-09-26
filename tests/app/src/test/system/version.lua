-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local system = require("system")
	local version: string = system.version()

	assert.eq(type(version), "string", "version is a string")
	assert.ok(#version > 0, "version is nonempty")
	assert.eq(select("#", system.version()), 1, "version has one result")
	assert.eq(system.version(), version, "version is repeatable")

	return true
end

return { main = main }
