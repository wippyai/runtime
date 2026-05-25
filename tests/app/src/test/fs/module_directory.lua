-- SPDX-License-Identifier: MPL-2.0

-- Test: a module-owned fs.directory with a relative path and no explicit base
-- resolves against the owning module's source root, not the process working
-- directory. The asset exists only under the module root, so a successful read
-- proves module-relative resolution (regression for project-root anchoring).
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, err = fs.get("wippy.fsdir:assets")
	assert.not_nil(vol, "module fs.directory available")
	assert.is_nil(err, "fs.get no error")

	local content, read_err = vol:readfile("/hello.txt")
	assert.is_nil(read_err, "readfile no error")
	assert.eq(content, "module-relative-resolution-ok", "reads asset from module source root")

	return true
end

return { main = main }
