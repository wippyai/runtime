-- SPDX-License-Identifier: MPL-2.0

-- Test: process.get_options and process.set_options
local assert = require("assert2")

local function main()
-- Test get_options exists
	assert.not_nil(process.get_options, "process.get_options exists")
	assert.is_function(process.get_options, "process.get_options is function")

	-- Test set_options exists
	assert.not_nil(process.set_options, "process.set_options exists")
	assert.is_function(process.set_options, "process.set_options is function")

	-- Get current options (should return table with trap_links)
	local opts = process.get_options()
	assert.not_nil(opts, "get_options returns value")
	assert.is_table(opts, "get_options returns table")

	-- Set valid options should succeed
	local ok, err = process.set_options({ trap_links = false })
	assert.ok(ok, "set_options succeeds")
	assert.is_nil(err, "set_options no error")

	-- Verify option was set
	local opts2 = process.get_options()
	assert.eq(opts2.trap_links, false, "trap_links was set to false")

	-- Set trap_links to true
	local ok2, err2 = process.set_options({ trap_links = true })
	assert.ok(ok2, "set_options trap_links=true succeeds")
	assert.is_nil(err2, "set_options trap_links=true no error")

	-- Verify option was set
	local opts3 = process.get_options()
	assert.eq(opts3.trap_links, true, "trap_links was set to true")

	return true
end

return { main = main }
