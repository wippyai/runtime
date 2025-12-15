-- Test: expr.eval function
local assert = require("assert2")
local expr = require("expr")

local function main()
    -- simple arithmetic
    local result, err = expr.eval("1 + 2")
    assert.is_nil(err, "eval simple no error")
    assert.eq(result, 3, "eval simple result")

    -- with environment variables
    result, err = expr.eval("x + y", {x = 10, y = 20})
    assert.is_nil(err, "eval with env no error")
    assert.eq(result, 30, "eval with env result")

    -- boolean operations
    result, err = expr.eval("true && false")
    assert.is_nil(err, "eval and no error")
    assert.eq(result, false, "eval and result")

    result, err = expr.eval("true || false")
    assert.is_nil(err, "eval or no error")
    assert.eq(result, true, "eval or result")

    -- string concatenation
    result, err = expr.eval('"hello" + " " + "world"')
    assert.is_nil(err, "eval string concat no error")
    assert.eq(result, "hello world", "eval string concat result")

    -- comparison operators
    result, err = expr.eval("x > 5", {x = 10})
    assert.is_nil(err, "eval gt no error")
    assert.eq(result, true, "eval gt result")

    result, err = expr.eval("x <= 5", {x = 5})
    assert.is_nil(err, "eval lte no error")
    assert.eq(result, true, "eval lte result")

    -- ternary operator
    result, err = expr.eval('x > 0 ? "positive" : "negative"', {x = 5})
    assert.is_nil(err, "eval ternary positive no error")
    assert.eq(result, "positive", "eval ternary positive result")

    result, err = expr.eval('x > 0 ? "positive" : "negative"', {x = -1})
    assert.is_nil(err, "eval ternary negative no error")
    assert.eq(result, "negative", "eval ternary negative result")

    -- builtin functions
    result, err = expr.eval("max(1, 5, 3)")
    assert.is_nil(err, "eval max no error")
    assert.eq(result, 5, "eval max result")

    result, err = expr.eval("min(1, 5, 3)")
    assert.is_nil(err, "eval min no error")
    assert.eq(result, 1, "eval min result")

    result, err = expr.eval("len([1, 2, 3])")
    assert.is_nil(err, "eval len no error")
    assert.eq(result, 3, "eval len result")

    -- nil evaluation
    result, err = expr.eval("nil")
    assert.is_nil(err, "eval nil no error")
    assert.is_nil(result, "eval nil result")

    return true
end

return { main = main }
