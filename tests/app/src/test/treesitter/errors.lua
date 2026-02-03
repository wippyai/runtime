-- Test: Tree-sitter error handling
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Test invalid language for parse
	local tree, err = treesitter.parse("nonexistent", "code")
	assert.is_nil(tree, "parse returns nil for invalid language")
	assert.not_nil(err, "error returned")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
	assert.eq(err:retryable(), false, "not retryable")

	-- Test invalid language for language()
	local lang, err2 = treesitter.language("nonexistent")
	assert.is_nil(lang, "language returns nil for invalid")
	assert.not_nil(err2, "error returned")
	assert.eq(err2:kind(), errors.INVALID, "error kind is INVALID")

	-- Test invalid query pattern
	local query, err3 = treesitter.query("go", "((invalid")
	assert.is_nil(query, "query returns nil for invalid pattern")
	assert.not_nil(err3, "error returned")
	assert.eq(err3:kind(), errors.INVALID, "error kind is INVALID")

	-- Test invalid language for query
	local query2, err4 = treesitter.query("nonexistent", "(identifier)")
	assert.is_nil(query2, "query returns nil for invalid language")
	assert.not_nil(err4, "error returned")
	assert.eq(err4:kind(), errors.INVALID, "error kind is INVALID")

	return true
end

return { main = main }
