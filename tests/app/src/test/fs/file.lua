-- Test: File read/write operations
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, err = fs.get("app:temp")
	assert.not_nil(vol, "temp fs available")
	assert.is_nil(err, "temp fs no error")

	local base = "/test_file_" .. os.time()

	-- writefile basic
	local ok, err = vol:writefile(base .. "_basic.txt", "hello world")
	assert.ok(ok, "writefile succeeds")
	assert.is_nil(err, "writefile no error")

	-- readfile
	local content, err = vol:readfile(base .. "_basic.txt")
	assert.eq(content, "hello world", "readfile matches")
	assert.is_nil(err, "readfile no error")

	-- writefile overwrite
	ok, err = vol:writefile(base .. "_basic.txt", "overwritten")
	assert.ok(ok, "writefile overwrite succeeds")

	content, err = vol:readfile(base .. "_basic.txt")
	assert.eq(content, "overwritten", "content overwritten")

	-- writefile append mode
	ok, err = vol:writefile(base .. "_append.txt", "first")
	assert.ok(ok, "create append file")

	ok, err = vol:writefile(base .. "_append.txt", " second", "a")
	assert.ok(ok, "append succeeds")

	content, err = vol:readfile(base .. "_append.txt")
	assert.eq(content, "first second", "content appended")

	-- writefile exclusive mode
	ok, err = vol:writefile(base .. "_excl.txt", "exclusive", "wx")
	assert.ok(ok, "exclusive write succeeds")

	ok, err = vol:writefile(base .. "_excl.txt", "fail", "wx")
	assert.eq(ok, false, "exclusive on existing fails")
	assert.not_nil(err, "exclusive error returned")

	-- open for reading
	local file, err = vol:open(base .. "_basic.txt", "r")
	assert.not_nil(file, "open for read")
	assert.is_nil(err, "open no error")

	local data, err = file:read(5)
	assert.eq(data, "overw", "read 5 bytes")
	assert.is_nil(err, "read no error")

	data, err = file:read(100)
	assert.eq(data, "ritten", "read rest")

	data, err = file:read(100)
	assert.is_nil(data, "read at EOF returns nil")
	assert.not_nil(err, "EOF error returned")
	-- V2 structured errors: check message contains EOF
	assert.ok(tostring(err):find("EOF"), "EOF error message")

	ok, err = file:close()
	assert.ok(ok, "close succeeds")

	-- open for writing
	file, err = vol:open(base .. "_write.txt", "w")
	assert.not_nil(file, "open for write")

	ok, err = file:write("written content")
	assert.ok(ok, "write succeeds")

	ok, err = file:sync()
	assert.ok(ok, "sync succeeds")

	ok, err = file:close()
	assert.ok(ok, "close write file")

	content, err = vol:readfile(base .. "_write.txt")
	assert.eq(content, "written content", "written content correct")

	-- file stat
	file, err = vol:open(base .. "_basic.txt", "r")
	assert.not_nil(file, "reopen file")

	local info, err = file:stat()
	assert.not_nil(info, "file stat")
	assert.eq(info.size, 11, "file size from stat")

	file:close()

	-- operations on closed file
	file, err = vol:open(base .. "_basic.txt", "r")
	file:close()

	data, err = file:read(10)
	assert.is_nil(data, "read closed returns nil")
	assert.not_nil(err, "read closed returns error")

	-- binary data using string.char
	local binary = string.char(0, 1, 2, 255, 254, 253)
	ok, err = vol:writefile(base .. "_binary.bin", binary)
	assert.ok(ok, "write binary")

	content, err = vol:readfile(base .. "_binary.bin")
	assert.eq(#content, 6, "binary length")
	assert.eq(content, binary, "binary content matches")

	-- Cleanup
	vol:remove(base .. "_basic.txt")
	vol:remove(base .. "_append.txt")
	vol:remove(base .. "_excl.txt")
	vol:remove(base .. "_write.txt")
	vol:remove(base .. "_binary.bin")

	return true
end

return { main = main }
