-- Test: template rendering with variables
local assert = require("assert2")
local templates = require("templates")

local function test_string_variable()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("greeting", {name = "Alice"})
    assert.is_nil(err, "render with string var")
    assert.eq(result, "Hello, Alice!", "string variable")

    set:release()
end

local function test_array_iteration()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("list", {items = {"apple", "banana", "cherry"}})
    assert.is_nil(err, "render with array")
    assert.eq(result, "Items: apple, banana, cherry", "array iteration")

    set:release()
end

local function test_empty_array()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("list", {items = {}})
    assert.is_nil(err, "render with empty array")
    assert.eq(result, "Items: ", "empty array")

    set:release()
end

local function test_single_item_array()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("list", {items = {"only"}})
    assert.is_nil(err, "render with single item")
    assert.eq(result, "Items: only", "single item array")

    set:release()
end

local function test_special_characters()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("greeting", {name = "<script>alert('xss')</script>"})
    assert.is_nil(err, "render with special chars")
    assert.not_nil(result, "result not nil")

    set:release()
end

local function main()
    test_string_variable()
    test_array_iteration()
    test_empty_array()
    test_single_item_array()
    test_special_characters()
    return true
end

return { main = main }
