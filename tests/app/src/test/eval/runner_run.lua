-- Test: eval_runner run function
local assert = require("assert")

local function main()
    local runner = require("eval_runner")
    local json = require("json")

    -- Test run with simple return
    local result, err = runner.run({
        source = [[
            local function handle(x)
                return x * 2
            end
            return { handle = handle }
        ]],
        method = "handle",
        args = { 21 },
        modules = { "json" }
    })

    assert.no_error(result, err, "run should succeed")
    assert.eq(result, 42, "should return 42")

    -- Test run with no args
    local result2, err2 = runner.run({
        source = [[
            return { main = function() return "hello" end }
        ]],
        method = "main",
        modules = {}
    })

    assert.no_error(result2, err2, "run without args should succeed")
    assert.eq(result2, "hello", "should return 'hello'")

    -- Test run with table result
    local result3, err3 = runner.run({
        source = [[
            return {
                get_data = function()
                    return { name = "test", value = 123 }
                end
            }
        ]],
        method = "get_data",
        modules = { "json" }
    })

    assert.no_error(result3, err3, "run with table result should succeed")
    assert.eq(result3.name, "test", "table result should have name")
    assert.eq(result3.value, 123, "table result should have value")

    -- Test run with json module
    local result4, err4 = runner.run({
        source = [[
            local json = require("json")
            return {
                encode = function(data)
                    return json.encode(data)
                end
            }
        ]],
        method = "encode",
        args = { { foo = "bar" } },
        modules = { "json" }
    })

    assert.no_error(result4, err4, "run with json should succeed")
    assert.is_string(result4, "result should be string")
    assert.contains(result4, "foo", "result should contain foo")
    assert.contains(result4, "bar", "result should contain bar")

    return true
end

return { main = main }
