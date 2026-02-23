-- SPDX-License-Identifier: MPL-2.0

-- Test: Path navigation (chdir/pwd)
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, _ = fs.get("app:temp")
	assert.not_nil(vol, "temp fs available")

	local base = "/test_path_" .. os.time()

	-- Create test structure
	vol:mkdir(base)
	vol:mkdir(base .. "/sub1")
	vol:mkdir(base .. "/sub1/sub2")
	vol:writefile(base .. "/root.txt", "root")
	vol:writefile(base .. "/sub1/file1.txt", "file1")
	vol:writefile(base .. "/sub1/sub2/file2.txt", "file2")

	-- Initial pwd
	local pwd, err = vol:pwd()
	assert.eq(pwd, "/", "initial pwd is /")
	assert.is_nil(err, "pwd no error")

	-- chdir to base
	local ok, _ = vol:chdir(base)
	assert.ok(ok, "chdir to base")

	pwd, _ = vol:pwd()
	assert.eq(pwd, base, "pwd is base")

	-- Relative path operations from base
	local exists, _ = vol:exists("root.txt")
	assert.ok(exists, "relative exists works")

	local content, _ = vol:readfile("root.txt")
	assert.eq(content, "root", "relative readfile works")

	-- chdir relative
	ok, _ = vol:chdir("sub1")
	assert.ok(ok, "chdir relative to sub1")

	pwd, _ = vol:pwd()
	assert.eq(pwd, base .. "/sub1", "pwd is sub1")

	content, _ = vol:readfile("file1.txt")
	assert.eq(content, "file1", "read file1 relative")

	-- chdir deeper
	ok, _ = vol:chdir("sub2")
	assert.ok(ok, "chdir to sub2")

	pwd, _ = vol:pwd()
	assert.eq(pwd, base .. "/sub1/sub2", "pwd is sub2")

	content, _ = vol:readfile("file2.txt")
	assert.eq(content, "file2", "read file2 relative")

	-- Absolute path still works
	content, _ = vol:readfile(base .. "/root.txt")
	assert.eq(content, "root", "absolute path works")

	-- chdir back to absolute
	ok, _ = vol:chdir("/")
	assert.ok(ok, "chdir to /")

	pwd, _ = vol:pwd()
	assert.eq(pwd, "/", "pwd back to /")

	-- chdir with leading slash (absolute)
	ok, _ = vol:chdir(base .. "/sub1")
	assert.ok(ok, "chdir absolute with slash")

	pwd, _ = vol:pwd()
	assert.eq(pwd, base .. "/sub1", "pwd absolute")

	-- Cleanup (use absolute paths)
	vol:chdir("/")
	vol:remove(base .. "/root.txt")
	vol:remove(base .. "/sub1/file1.txt")
	vol:remove(base .. "/sub1/sub2/file2.txt")
	vol:remove(base .. "/sub1/sub2")
	vol:remove(base .. "/sub1")
	vol:remove(base)

	return true
end

return { main = main }
