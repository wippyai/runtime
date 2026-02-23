-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- basic parse with namespace
	local id = registry.parse_id("test:example")
	assert.not_nil(id, "parse_id returned table")
	assert.eq(id.ns, "test", "namespace is 'test'")
	assert.eq(id.name, "example", "name is 'example'")

	-- parse without namespace
	id = registry.parse_id("justname")
	assert.eq(id.ns, "", "namespace is empty")
	assert.eq(id.name, "justname", "name is full string")

	-- parse with path in name
	id = registry.parse_id("app:path/to/function")
	assert.eq(id.ns, "app", "namespace is 'app'")
	assert.eq(id.name, "path/to/function", "name includes path")

	-- parse with multiple colons
	id = registry.parse_id("ns:name:with:colons")
	assert.eq(id.ns, "ns", "namespace is first part")
	assert.eq(id.name, "name:with:colons", "name includes rest")

	-- empty string
	id = registry.parse_id("")
	assert.eq(id.ns, "", "namespace empty for empty string")
	assert.eq(id.name, "", "name empty for empty string")

	return true
end

return { main = main }
