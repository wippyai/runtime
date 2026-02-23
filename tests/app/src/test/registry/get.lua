-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- get an existing entry from the registry
	local entry, err = registry.get("app.lib:assert")
	assert.is_nil(err, "get app.lib:assert no error")
	assert.not_nil(entry, "get returns entry")
	assert.eq(type(entry), "table", "entry is table")

	-- entry has id as string
	assert.not_nil(entry.id, "entry has id")
	assert.eq(type(entry.id), "string", "id is string")
	assert.eq(entry.id, "app.lib:assert", "entry id is correct")

	-- entry has kind
	assert.not_nil(entry.kind, "entry has kind")
	assert.eq(type(entry.kind), "string", "kind is string")

	-- entry has meta
	assert.not_nil(entry.meta, "entry has meta")
	assert.eq(type(entry.meta), "table", "meta is table")

	-- entry has data
	assert.not_nil(entry.data, "entry has data")
	assert.eq(type(entry.data), "table", "data is table")

	-- parse the id to get namespace and name
	local id = registry.parse_id(entry.id)
	assert.eq(id.ns, "app.lib", "parsed namespace")
	assert.eq(id.name, "assert", "parsed name")

	return true
end

return { main = main }
