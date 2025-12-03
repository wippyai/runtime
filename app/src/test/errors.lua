-- Comprehensive test for structured errors
-- Tests: errors.new, errors.wrap, kind, retryable, details, message, stack
-- Tests: string concatenation, tostring, errors.is, errors.call_stack

local function eq(actual, expected, msg)
    if actual ~= expected then
        error((msg or "assertion failed") .. ": expected " .. tostring(expected) .. ", got " .. tostring(actual), 2)
    end
end

local function truthy(val, msg)
    if not val then
        error((msg or "expected truthy value") .. ", got: " .. tostring(val), 2)
    end
end

local function falsy(val, msg)
    if val then
        error((msg or "expected falsy value") .. ", got: " .. tostring(val), 2)
    end
end

local function main()
    -- Test errors.new with string
    local e1 = errors.new("simple error")
    truthy(e1, "errors.new should return error")
    eq(e1:message(), "simple error", "message should match")
    eq(e1:kind(), "", "default kind should be empty string")
    eq(e1:retryable(), nil, "default retryable should be nil")
    eq(e1:details(), nil, "default details should be nil")

    -- Test errors.new with table
    local e2 = errors.new({
        message = "not found",
        kind = errors.NOT_FOUND,
        retryable = false,
        details = {
            resource = "user",
            id = 123
        }
    })
    eq(e2:message(), "not found", "message from table")
    eq(e2:kind(), "NotFound", "kind from table")
    eq(e2:retryable(), false, "retryable from table")
    local d = e2:details()
    truthy(d, "details should exist")
    eq(d.resource, "user", "details.resource")
    eq(type(d.id), "number", "d.id should be number")
    eq(d.id, 123, "details.id")

    -- Test all kind constants
    eq(errors.NOT_FOUND, "NotFound", "NOT_FOUND constant")
    eq(errors.ALREADY_EXISTS, "AlreadyExists", "ALREADY_EXISTS constant")
    eq(errors.INVALID, "Invalid", "INVALID constant")
    eq(errors.PERMISSION_DENIED, "PermissionDenied", "PERMISSION_DENIED constant")
    eq(errors.UNAVAILABLE, "Unavailable", "UNAVAILABLE constant")
    eq(errors.INTERNAL, "Internal", "INTERNAL constant")
    eq(errors.CANCELED, "Canceled", "CANCELED constant")
    eq(errors.CONFLICT, "Conflict", "CONFLICT constant")
    eq(errors.TIMEOUT, "Timeout", "TIMEOUT constant")
    eq(errors.RATE_LIMITED, "RateLimited", "RATE_LIMITED constant")
    eq(errors.UNKNOWN, "", "UNKNOWN constant is empty string")

    -- Test errors.is
    local e3 = errors.new({message = "timeout", kind = errors.TIMEOUT})
    eq(errors.is(e3, "Timeout"), true, "errors.is should match")
    eq(errors.is(e3, "NotFound"), false, "errors.is should not match wrong kind")
    eq(errors.is("not an error", "Timeout"), false, "errors.is should return false for non-error")

    -- Test tostring
    local str = tostring(e1)
    truthy(str, "tostring should return string")
    truthy(string.find(str, "simple error", 1, true), "tostring should contain message")

    -- Test string concatenation (both directions)
    local concat1 = "prefix: " .. e1
    truthy(string.find(concat1, "prefix:", 1, true), "concat should have prefix")
    truthy(string.find(concat1, "simple error", 1, true), "concat should have error message")

    local concat2 = e1 .. " :suffix"
    truthy(string.find(concat2, ":suffix", 1, true), "concat should have suffix")
    truthy(string.find(concat2, "simple error", 1, true), "concat should have error message")

    -- Test error + error concatenation
    local concat3 = e1 .. e2
    truthy(string.find(concat3, "simple error", 1, true), "concat should have first error")
    truthy(string.find(concat3, "not found", 1, true), "concat should have second error")

    -- Test errors.wrap
    local inner = errors.new({
        message = "inner error",
        kind = errors.INVALID,
        retryable = true,
        details = {code = 42}
    })
    local outer = errors.wrap(inner, "outer context")
    eq(outer:kind(), "Invalid", "wrap should preserve kind")
    eq(outer:retryable(), true, "wrap should preserve retryable")
    local od = outer:details()
    truthy(od, "wrap should preserve details")
    eq(od.code, 42, "wrap should preserve details values")
    eq(outer:message(), "inner error", "wrap preserves original message")
    eq(tostring(outer), "inner error", "tostring returns original message")

    -- Test errors.wrap with string
    local wrapped_str = errors.wrap("string error", "context")
    truthy(wrapped_str, "wrap string should work")
    eq(wrapped_str:message(), "string error", "wrapped string preserves message")

    -- Test stack trace
    local function inner_func()
        return errors.new("stack test")
    end
    local function outer_func()
        return inner_func()
    end
    local stack_err = outer_func()
    local stack = stack_err:stack()
    truthy(stack, "stack should exist")
    truthy(#stack > 0, "stack should not be empty")

    -- Test errors.call_stack
    local cs = errors.call_stack(stack_err)
    truthy(cs, "call_stack should return table")
    truthy(cs.frames, "call_stack should have frames")
    truthy(#cs.frames > 0, "frames should not be empty")
    local frame = cs.frames[1]
    truthy(frame.line, "frame should have line")
    truthy(frame.source, "frame should have source")

    -- Test retryable variations
    local e_retry_true = errors.new({message = "retry", retryable = true})
    eq(e_retry_true:retryable(), true, "retryable true")

    local e_retry_false = errors.new({message = "no retry", retryable = false})
    eq(e_retry_false:retryable(), false, "retryable false")

    local e_retry_nil = errors.new("unknown retry")
    eq(e_retry_nil:retryable(), nil, "retryable nil/unknown")

    -- Test nested details
    local e_nested = errors.new({
        message = "nested",
        details = {
            user = {name = "test", id = 1},
            count = 5
        }
    })
    local nd = e_nested:details()
    truthy(nd, "nested details should exist")
    eq(nd.count, 5, "nested details primitive")
    truthy(nd.user, "nested details table")

    -- Test empty details vs nil details
    local e_empty = errors.new({message = "empty details", details = {}})
    eq(e_empty:details(), nil, "empty details should be nil")

    return true
end

return { main = main }
