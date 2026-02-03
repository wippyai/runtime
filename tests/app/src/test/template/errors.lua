-- Test: template error handling
local assert = require("assert2")
local templates = require("templates")

local function test_template_not_found()
	local set, err = templates.get("app.test.template:test_set")
	assert.is_nil(err, "get template set")

	local result, err = set:render("nonexistent", {})
	assert.is_nil(result, "result nil for missing template")
	assert.not_nil(err, "error for missing template")
	assert.eq(err:kind(), errors.NOT_FOUND, "error kind is NOT_FOUND")

	set:release()
end

local function test_set_not_found()
	local set, err = templates.get("app.test.template:nonexistent_set")
	assert.is_nil(set, "set nil for missing set")
	assert.not_nil(err, "error for missing set")
end

local function test_render_after_release()
	local set, err = templates.get("app.test.template:test_set")
	assert.is_nil(err, "get template set")

	set:release()

	local result, err = set:render("greeting", {name = "Test"})
	assert.is_nil(result, "result nil after release")
	assert.not_nil(err, "error after release")
	assert.contains(tostring(err), "released", "error mentions released")
end

local function test_empty_template_name()
	local set, err = templates.get("app.test.template:test_set")
	assert.is_nil(err, "get template set")

	local result, err = set:render("", {})
	assert.is_nil(result, "result nil for empty name")
	assert.not_nil(err, "error for empty name")

	set:release()
end

local function main()
	test_template_not_found()
	test_set_not_found()
	test_render_after_release()
	test_empty_template_name()
	return true
end

return { main = main }
