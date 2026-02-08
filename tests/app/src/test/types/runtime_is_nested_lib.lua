local assert = require("assert2")
local inner = require("inner")
local outer = require("outer")

local function main(): boolean
	local inner_ok, inner_err = outer.Inner:is({id = "ok", flags = {"hot"}})
	assert.not_nil(inner_ok, "Inner valid should pass")
	assert.is_nil(inner_err, "Inner valid should have nil error")

	local inner_bad, inner_bad_err = outer.Inner:is({id = "x", flags = {}})
	assert.is_nil(inner_bad, "Inner invalid should fail")
	assert.not_nil(inner_bad_err, "Inner invalid should return error")
	assert.error_contains(inner_bad_err, "length", "Inner constraints should mention length")

	local wrapped = outer.wrap(inner.make_inner("ok", {"warm"}), "tag")
	local outer_ok, outer_err = outer.Outer:is(wrapped)
	assert.not_nil(outer_ok, "Outer valid should pass")
	assert.is_nil(outer_err, "Outer valid should have nil error")

	local outer_bad_label, outer_bad_label_err = outer.Outer:is({
		inner = inner.make_inner("ok", {"hot"}),
		label = ""
	})
	assert.is_nil(outer_bad_label, "Outer with empty label should fail")
	assert.not_nil(outer_bad_label_err, "Outer with empty label should return error")
	assert.error_contains(outer_bad_label_err, "length", "Label min_len should mention length")

	local list_ok, list_err = outer.OuterList:is({wrapped})
	assert.not_nil(list_ok, "OuterList valid should pass")
	assert.is_nil(list_err, "OuterList valid should have nil error")

	local list_bad, list_bad_err = outer.OuterList:is({})
	assert.is_nil(list_bad, "OuterList empty should fail")
	assert.not_nil(list_bad_err, "OuterList empty should return error")
	assert.error_contains(list_bad_err, "length", "OuterList min_len should mention length")

	return true
end

return { main = main }
