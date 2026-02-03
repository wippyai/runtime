-- Test: text.diff functionality
local assert = require("assert2")

local function main()
	local text = require("text")

	-- Test diff.new
	local diff, err = text.diff.new()
	assert.is_nil(err, "diff.new should not error")
	assert.not_nil(diff, "differ object returned")

	-- Test diff.new with options
	local diff_opts, err2 = text.diff.new({
		diff_timeout = 2.0,
		match_threshold = 0.5
	})
	assert.is_nil(err2, "diff.new with options should not error")
	assert.not_nil(diff_opts, "differ with options returned")

	-- Test compare - identical
	local diffs_same, err3 = diff:compare("same text", "same text")
	assert.is_nil(err3, "compare should not error")
	assert.eq(#diffs_same, 1, "identical texts produce 1 diff")
	assert.eq(diffs_same[1].operation, "equal", "operation is equal")
	assert.eq(diffs_same[1].text, "same text", "text matches")

	-- Test compare - different
	local diffs, err4 = diff:compare("hello world", "hello there")
	assert.is_nil(err4, "compare should not error")
	assert.ok(#diffs > 1, "different texts produce multiple diffs")

	local has_equal, has_delete, has_insert = false, false, false
	for _, d in ipairs(diffs) do
		if d.operation == "equal" then
			has_equal = true
		end
		if d.operation == "delete" then
			has_delete = true
		end
		if d.operation == "insert" then
			has_insert = true
		end
	end
	assert.ok(has_equal, "has equal operation")
	assert.ok(has_delete, "has delete operation")
	assert.ok(has_insert, "has insert operation")

	-- Test summarize
	local summary = diff:summarize(diffs)
	assert.not_nil(summary, "summary returned")
	assert.ok(type(summary.insertions) == "number", "insertions is number")
	assert.ok(type(summary.deletions) == "number", "deletions is number")
	assert.ok(type(summary.equals) == "number", "equals is number")
	assert.ok(summary.insertions > 0, "has insertions")
	assert.ok(summary.deletions > 0, "has deletions")

	-- Test pretty_text
	local pretty, err5 = diff:pretty_text(diffs)
	assert.is_nil(err5, "pretty_text should not error")
	assert.ok(#pretty > 0, "pretty text not empty")

	-- Test pretty_html
	local html, err6 = diff:pretty_html(diffs)
	assert.is_nil(err6, "pretty_html should not error")
	assert.ok(#html > 0, "html not empty")

	-- Test patch_make and patch_apply
	local text1 = "The quick brown fox jumps over the lazy dog"
	local text2 = "The quick red fox jumps over the lazy cat"

	local patches, err7 = diff:patch_make(text1, text2)
	assert.is_nil(err7, "patch_make should not error")
	assert.ok(#patches > 0, "patches created")

	local result, success = diff:patch_apply(patches, text1)
	assert.ok(success, "patch apply succeeded")
	assert.eq(result, text2, "patched text equals text2")

	return true
end

return { main = main }
