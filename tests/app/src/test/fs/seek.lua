-- Test: File seek operations
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, _ = fs.get("app:temp")
	assert.not_nil(vol, "temp fs available")

	local path = "/test_seek_" .. os.time() .. ".txt"

	-- Create test file
	local ok, _ = vol:writefile(path, "0123456789ABCDEF")
	assert.ok(ok, "create test file")

	-- Open for reading
	local file, _ = vol:open(path, "r")
	assert.not_nil(file, "open file")

	-- Read from start
	local data, _ = file:read(4)
	assert.eq(data, "0123", "read first 4")

	-- Seek to position 8 from start
	local pos, err = file:seek("set", 8)
	assert.eq(pos, 8, "seek set returns position")
	assert.is_nil(err, "seek no error")

	data, _ = file:read(4)
	assert.eq(data, "89AB", "read after seek set")

	-- Seek relative (current position is now 12)
	pos, _ = file:seek("cur", -4)
	assert.eq(pos, 8, "seek cur back")

	data, _ = file:read(2)
	assert.eq(data, "89", "read after seek cur")

	-- Seek from end
	pos, _ = file:seek("end", -4)
	assert.eq(pos, 12, "seek end")

	data, _ = file:read(4)
	assert.eq(data, "CDEF", "read from end")

	-- Seek to start
	pos, _ = file:seek("set", 0)
	assert.eq(pos, 0, "seek to start")

	data, _ = file:read(2)
	assert.eq(data, "01", "read from start")

	-- Current position without offset
	pos, _ = file:seek("cur", 0)
	assert.eq(pos, 2, "get current position")

	file:close()

	-- Seek constants
	assert.eq(fs.seek.SET, "set", "seek.SET constant")
	assert.eq(fs.seek.CUR, "cur", "seek.CUR constant")
	assert.eq(fs.seek.END, "end", "seek.END constant")

	-- Seek on write file
	file, _ = vol:open(path, "w")
	assert.not_nil(file, "open for write")

	ok, _ = file:write("AAAA")
	assert.ok(ok, "write AAAA")

	pos, _ = file:seek("set", 0)
	assert.eq(pos, 0, "seek to start in write mode")

	ok, _ = file:write("BB")
	assert.ok(ok, "overwrite with BB")

	file:close()

	local content, _ = vol:readfile(path)
	assert.eq(content, "BBAA", "overwritten content")

	-- Invalid whence
	file, _ = vol:open(path, "r")
	pos, err = file:seek("invalid", 0)
	assert.is_nil(pos, "invalid whence returns nil")
	assert.not_nil(err, "invalid whence returns error")

	file:close()

	-- Cleanup
	vol:remove(path)

	return true
end

return { main = main }
