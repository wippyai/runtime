-- SPDX-License-Identifier: MPL-2.0

-- Test: Directory operations
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, err = fs.get("app:temp")
	assert.not_nil(vol, "temp fs available")
	assert.is_nil(err, "temp fs no error")

	local base = "/test_dir_" .. os.time()

	-- mkdir
	local ok, err = vol:mkdir(base)
	assert.ok(ok, "mkdir succeeds")
	assert.is_nil(err, "mkdir no error")

	-- exists
	local exists, err = vol:exists(base)
	assert.ok(exists, "directory exists")
	assert.is_nil(err, "exists no error")

	-- isdir
	local isdir, err = vol:isdir(base)
	assert.ok(isdir, "path is directory")
	assert.is_nil(err, "isdir no error")

	-- stat directory
	local info, err = vol:stat(base)
	assert.not_nil(info, "stat returns info")
	assert.is_nil(err, "stat no error")
	assert.ok(info.is_dir, "stat shows is_dir")
	assert.eq(info.type, fs.type.DIR, "type is DIR")

	-- Create nested structure
	ok, err = vol:mkdir(base .. "/subdir")
	assert.ok(ok, "nested mkdir succeeds")

	ok, err = vol:writefile(base .. "/file1.txt", "content1")
	assert.ok(ok, "create file1")

	ok, err = vol:writefile(base .. "/file2.txt", "content2")
	assert.ok(ok, "create file2")

	ok, err = vol:writefile(base .. "/subdir/nested.txt", "nested")
	assert.ok(ok, "create nested file")

	-- Verify structure
	exists, err = vol:exists(base .. "/subdir")
	assert.ok(exists, "subdir exists")

	exists, err = vol:exists(base .. "/file1.txt")
	assert.ok(exists, "file1 exists")

	-- isdir on file
	isdir, err = vol:isdir(base .. "/file1.txt")
	assert.eq(isdir, false, "file is not directory")

	-- stat file
	info, err = vol:stat(base .. "/file1.txt")
	assert.not_nil(info, "file stat returns info")
	assert.eq(info.is_dir, false, "file stat shows not dir")
	assert.eq(info.type, fs.type.FILE, "type is FILE")
	assert.eq(info.size, 8, "file size correct")
	assert.not_nil(info.name, "has name")
	assert.not_nil(info.mode, "has mode")
	assert.not_nil(info.modified, "has modified time")

	-- Remove files
	ok, err = vol:remove(base .. "/file1.txt")
	assert.ok(ok, "remove file1")

	ok, err = vol:remove(base .. "/file2.txt")
	assert.ok(ok, "remove file2")

	ok, err = vol:remove(base .. "/subdir/nested.txt")
	assert.ok(ok, "remove nested")

	ok, err = vol:remove(base .. "/subdir")
	assert.ok(ok, "remove subdir")

	ok, err = vol:remove(base)
	assert.ok(ok, "remove base dir")

	-- Verify cleanup
	exists, err = vol:exists(base)
	assert.eq(exists, false, "base dir removed")

	return true
end

return { main = main }
