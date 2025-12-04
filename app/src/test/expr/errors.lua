-- Test: expr error handling
local assert = require("assert2")
local expr = require("expr")

local function main()
    -- empty expression error - INVALID
    local result, err = expr.eval("")
    assert.is_nil(result, "empty expression returns nil")
    assert.not_nil(err, "empty expression has error")
    assert.eq(err:kind(), errors.INVALID, "empty expression kind is INVALID")
    assert.eq(err:retryable(), false, "empty expression not retryable")

    -- invalid syntax error - INTERNAL (compile error)
    result, err = expr.eval("invalid syntax !!!")
    assert.is_nil(result, "invalid syntax returns nil")
    assert.not_nil(err, "invalid syntax has error")
    assert.eq(err:kind(), errors.INTERNAL, "invalid syntax kind is INTERNAL")
    assert.eq(err:retryable(), false, "invalid syntax not retryable")

    -- compile empty expression - INVALID
    local program, err = expr.compile("")
    assert.is_nil(program, "compile empty returns nil")
    assert.not_nil(err, "compile empty has error")
    assert.eq(err:kind(), errors.INVALID, "compile empty kind is INVALID")
    assert.eq(err:retryable(), false, "compile empty not retryable")

    -- compile invalid syntax - INTERNAL
    program, err = expr.compile("(((")
    assert.is_nil(program, "compile invalid returns nil")
    assert.not_nil(err, "compile invalid has error")
    assert.eq(err:kind(), errors.INTERNAL, "compile invalid kind is INTERNAL")

    -- run with missing variable - INTERNAL
    program, err = expr.compile("x + y")
    assert.is_nil(err, "compile x+y succeeds")
    assert.not_nil(program, "compile x+y returns program")

    result, err = program:run({x = 10})
    assert.is_nil(result, "missing var returns nil")
    assert.not_nil(err, "missing var has error")
    assert.eq(err:kind(), errors.INTERNAL, "missing var kind is INTERNAL")
    assert.eq(err:retryable(), false, "missing var not retryable")

    -- verify error has string representation
    local err_str = tostring(err)
    assert.not_nil(err_str, "error has tostring")
    assert.ok(#err_str > 0, "error string not empty")

    -- error can be concatenated
    local msg = "Error: " .. err
    assert.ok(string.find(msg, "Error:", 1, true), "error can be concatenated")

    return true
end

return { main = main }
