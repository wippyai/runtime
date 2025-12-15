-- Test: template inheritance (extends/blocks)
local assert = require("assert2")
local templates = require("templates")

local function test_extends_base()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    local result, err = set:render("page", {
        title = "My Page",
        heading = "Welcome",
        bodyText = "This is the page content."
    })
    assert.is_nil(err, "render extended template")

    assert.contains(result, "<title>My Page</title>", "title block")
    assert.contains(result, "<h1>Welcome</h1>", "heading in content")
    assert.contains(result, "<p>This is the page content.</p>", "bodyText in content")
    assert.contains(result, "<!DOCTYPE html>", "doctype from base")
    assert.contains(result, "</html>", "closing html from base")

    set:release()
end

local function test_base_template_not_renderable_directly()
    local set, err = templates.get("app.test.template:test_set")
    assert.is_nil(err, "get template set")

    -- Base templates with yield blocks cannot be rendered directly
    local result, err = set:render("base", {})
    assert.is_nil(result, "result nil for base template")
    assert.not_nil(err, "error for base template with yield blocks")

    set:release()
end

local function main()
    test_extends_base()
    test_base_template_not_renderable_directly()
    return true
end

return { main = main }
