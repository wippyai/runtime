-- Test: Directory iteration
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, _ = fs.get("app:temp")
	assert.not_nil(vol, "temp fs available")

	local base = "/test_readdir_" .. os.time()

	-- Create test structure
	local ok, _ = vol:mkdir(base)
	assert.ok(ok, "create base dir")

	vol:writefile(base .. "/file1.txt", "a")
	vol:writefile(base .. "/file2.txt", "bb")
	vol:writefile(base .. "/file3.txt", "ccc")
	vol:mkdir(base .. "/subdir1")
	vol:mkdir(base .. "/subdir2")

	-- Iterate directory
	local iter, state = vol:readdir(base)
	assert.not_nil(iter, "readdir returns iterator")

	local entries: {[string]: string} = {}
	for entry in iter, state do
		entries[entry.name] = entry.type
	end

	-- Verify entries
	assert.eq(entries["file1.txt"], fs.type.FILE, "file1 is FILE")
	assert.eq(entries["file2.txt"], fs.type.FILE, "file2 is FILE")
	assert.eq(entries["file3.txt"], fs.type.FILE, "file3 is FILE")
	assert.eq(entries["subdir1"], fs.type.DIR, "subdir1 is DIR")
	assert.eq(entries["subdir2"], fs.type.DIR, "subdir2 is DIR")

	-- Count entries
	local count = 0
	for _ in pairs(entries) do
		count = count + 1
	end
	assert.eq(count, 5, "5 entries total")

	-- Empty directory
	local empty = base .. "/empty"
	vol:mkdir(empty)

	iter, state = vol:readdir(empty)
	assert.not_nil(iter, "readdir empty returns iterator")

	count = 0
	for _ in iter, state do
		count = count + 1
	end
	assert.eq(count, 0, "empty dir has 0 entries")

	-- Type constants
	assert.eq(fs.type.FILE, "file", "type.FILE constant")
	assert.eq(fs.type.DIR, "directory", "type.DIR constant")

	-- Cleanup
	vol:remove(base .. "/file1.txt")
	vol:remove(base .. "/file2.txt")
	vol:remove(base .. "/file3.txt")
	vol:remove(base .. "/subdir1")
	vol:remove(base .. "/subdir2")
	vol:remove(empty)
	vol:remove(base)

	return true
end

return { main = main }
