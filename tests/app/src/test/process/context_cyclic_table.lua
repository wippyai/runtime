-- SPDX-License-Identifier: MPL-2.0

-- Test: Lua->Go conversion handles cyclic tables at process context boundaries.
local assert = require("assert2")

local function main()
	local root = {
		name = "root",
		nested = {
			value = 42
		}
	}
	root.self = root
	root.nested.parent = root

	local spawner = process.with_context({
		cyclic = root
	})
	assert.not_nil(spawner, "with_context returns spawner for cyclic table")

	return true
end

return { main = main }
