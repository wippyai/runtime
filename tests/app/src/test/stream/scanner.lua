-- SPDX-License-Identifier: MPL-2.0

-- Test: Stream scanner operations
local assert = require("assert2")

local function main()
	local fs = require("fs")

	local vol, err = fs.get("app:temp")
	assert.not_nil(vol, "temp fs available")
	assert.is_nil(err, "temp fs no error")

	local base = "/test_scanner_" .. os.time()

	-- Create test file with multiple lines
	local content = "line one\nline two\nline three\n"
	local ok, _ = vol:writefile(base .. "_lines.txt", content)
	assert.ok(ok, "writefile succeeds")

	-- Open file and create scanner
	local file, err = vol:open(base .. "_lines.txt", "r")
	assert.not_nil(file, "open file for read")
	assert.is_nil(err, "open no error")

	-- Create scanner (default split by lines)
	local scanner = file:scanner()
	assert.not_nil(scanner, "scanner created")

	-- Scan first line
	local has_token = scanner:scan()
	assert.ok(has_token, "scan returns true for first line")
	assert.eq(scanner:text(), "line one", "first line text")
	assert.is_nil(scanner:err(), "no error on first scan")

	-- Scan second line
	has_token = scanner:scan()
	assert.ok(has_token, "scan returns true for second line")
	assert.eq(scanner:text(), "line two", "second line text")

	-- Scan third line
	has_token = scanner:scan()
	assert.ok(has_token, "scan returns true for third line")
	assert.eq(scanner:text(), "line three", "third line text")

	-- Scan past EOF
	has_token = scanner:scan()
	assert.eq(has_token, false, "scan returns false at EOF")
	assert.is_nil(scanner:err(), "no error at EOF")

	file:close()

	-- Test scanner with split by words
	content = "hello world foo bar"
	ok, err = vol:writefile(base .. "_words.txt", content)
	assert.ok(ok, "writefile words succeeds")

	file, err = vol:open(base .. "_words.txt", "r")
	assert.not_nil(file, "open words file")

	-- Create scanner with word split
	scanner = file:scanner("words")
	assert.not_nil(scanner, "word scanner created")

	has_token = scanner:scan()
	assert.ok(has_token, "scan first word")
	assert.eq(scanner:text(), "hello", "first word")

	has_token = scanner:scan()
	assert.ok(has_token, "scan second word")
	assert.eq(scanner:text(), "world", "second word")

	has_token = scanner:scan()
	assert.ok(has_token, "scan third word")
	assert.eq(scanner:text(), "foo", "third word")

	has_token = scanner:scan()
	assert.ok(has_token, "scan fourth word")
	assert.eq(scanner:text(), "bar", "fourth word")

	has_token = scanner:scan()
	assert.eq(has_token, false, "words EOF")

	file:close()

	-- Cleanup
	vol:remove(base .. "_lines.txt")
	vol:remove(base .. "_words.txt")

	return true
end

return { main = main }
