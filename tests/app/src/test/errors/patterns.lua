-- Test: idiomatic error handling patterns
local assert = require("assert2")

-- Pattern: function returning different error kinds
local function fetch_user(id)
    if id < 0 then
        return nil, errors.new({
            message = "invalid user id",
            kind = errors.INVALID,
            retryable = false
        })
    elseif id == 0 then
        return nil, errors.new({
            message = "user not found",
            kind = errors.NOT_FOUND,
            retryable = false
        })
    elseif id == 999 then
        return nil, errors.new({
            message = "database unavailable",
            kind = errors.UNAVAILABLE,
            retryable = true
        })
    end
    return {id = id, name = "test"}
end

-- Pattern: switch on error kind
local function handle_error(err)
    local kind = err:kind()
    if kind == errors.NOT_FOUND then
        return "not_found"
    elseif kind == errors.INVALID then
        return "invalid"
    elseif kind == errors.PERMISSION_DENIED then
        return "denied"
    elseif kind == errors.UNAVAILABLE then
        return "unavailable"
    else
        return "unknown"
    end
end

-- Pattern: retry on retryable errors
local function with_retry(fn, max_retries)
    local result, err
    for _ = 1, max_retries do
        result, err = fn()
        if not err then
            return result
        end
        if not err:retryable() then
            return nil, err
        end
    end
    return nil, err
end

local function main()
    -- Test kind checking
    local user, err = fetch_user(-1)
    assert.is_nil(user, "no user on error")
    assert.eq(err:kind(), errors.INVALID, "INVALID kind")
    assert.eq(err:retryable(), false, "not retryable")

    user, err = fetch_user(0)
    assert.is_nil(user, "no user for NOT_FOUND")
    assert.eq(err:kind(), errors.NOT_FOUND, "NOT_FOUND kind")

    user, err = fetch_user(999)
    assert.is_nil(user, "no user for UNAVAILABLE")
    assert.eq(err:kind(), errors.UNAVAILABLE, "UNAVAILABLE kind")
    assert.eq(err:retryable(), true, "is retryable")

    user, err = fetch_user(1)
    assert.is_nil(err, "no error on success")
    assert.eq(user.id, 1, "user returned")

    -- Test switch pattern
    assert.eq(handle_error(errors.new({message = "test", kind = errors.NOT_FOUND})), "not_found")
    assert.eq(handle_error(errors.new({message = "test", kind = errors.INVALID})), "invalid")
    assert.eq(handle_error(errors.new({message = "test"})), "unknown")

    -- Test retry pattern
    local attempts = 0
    local function flaky()
        attempts = attempts + 1
        if attempts < 3 then
            return nil, errors.new({
                message = "temp fail",
                kind = errors.UNAVAILABLE,
                retryable = true
            })
        end
        return "success"
    end

    local result = with_retry(flaky, 5)
    assert.eq(result, "success", "succeeds after retries")
    assert.eq(attempts, 3, "took 3 attempts")

    -- Test wrap preserves kind
    local inner = errors.new({message = "not found", kind = errors.NOT_FOUND})
    local outer = errors.wrap(inner, "context")
    assert.eq(outer:kind(), errors.NOT_FOUND, "wrap preserves kind")

    return true
end

return { main = main }
