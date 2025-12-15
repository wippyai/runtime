-- Test: error retryable flag
local assert = require("assert2")

local function main()
    local e_true = errors.new({message = "retry", retryable = true})
    assert.eq(e_true:retryable(), true, "retryable true")

    local e_false = errors.new({message = "no retry", retryable = false})
    assert.eq(e_false:retryable(), false, "retryable false")

    local e_nil = errors.new("unknown retry")
    assert.is_nil(e_nil:retryable(), "retryable nil when not set")

    return true
end

return { main = main }
