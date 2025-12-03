-- Test: error stack traces
local assert = require("assert2")

local function main()
    local function inner_func()
        return errors.new("stack test")
    end
    local function outer_func()
        return inner_func()
    end

    local err = outer_func()

    -- err:stack() returns string
    local stack = err:stack()
    assert.ok(stack, "stack exists")
    assert.ok(#stack > 0, "stack not empty")

    -- errors.call_stack returns structured data
    local cs = errors.call_stack(err)
    assert.ok(cs, "call_stack returns table")
    assert.ok(cs.frames, "has frames")
    assert.ok(#cs.frames > 0, "frames not empty")

    local frame = cs.frames[1]
    assert.ok(frame.line, "frame has line")
    assert.ok(frame.source, "frame has source")

    return true
end

return { main = main }
