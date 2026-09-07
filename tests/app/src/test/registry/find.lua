-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- find by kind
	local entries, err = registry.find({kind = "function.lua"})
	assert.is_nil(err, "find by kind no error")
	assert.not_nil(entries, "find returns entries")
	assert.eq(type(entries), "table", "entries is table")

	-- find by type in meta
	entries, err = registry.find({type = "test"})
	assert.is_nil(err, "find by type no error")
	assert.not_nil(entries, "find by type returns entries")
	assert.ok(#entries > 0, "find by type has results")

	-- each entry has expected structure
	for _, entry in ipairs(entries) do
		assert.not_nil(entry.id, "entry has id")
		assert.eq(type(entry.id), "string", "id is string")
		assert.not_nil(entry.kind, "entry has kind")
	end

	-- find returns table even with empty filter
	entries, err = registry.find({})
	assert.is_nil(err, "find empty filter no error")
	assert.not_nil(entries, "find returns table")
	assert.eq(type(entries), "table", "result is table")

	return true
end

return { main = main }
