-- Test: basic template rendering
local assert = require("assert2")
local templates = require("templates")

local function test_get_template_set()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")
    assert.not_nil(set, "template set not nil")
    set:release()
end

local function test_simple_render()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("greeting", {name = "World"})
    assert.is_nil(err, "render greeting")
    assert.eq(result, "Hello, World!", "greeting output")

    set:release()
end

local function test_render_with_globals()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("with_globals", {})
    assert.is_nil(err, "render with globals")
    assert.eq(result, "Site: Test Site, Version: 1.0.0", "globals output")

    set:release()
end

local function test_tostring()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local str = tostring(set)
    assert.eq(str, "template.Set{}", "tostring before release")

    set:release()

    str = tostring(set)
    assert.eq(str, "template.Set{released}", "tostring after release")
end

local function main()
    test_get_template_set()
    test_simple_render()
    test_render_with_globals()
    test_tostring()
    return true
end

return { main = main }
