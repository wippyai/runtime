-- Test assertion library
local M = {}

function M.eq(actual: any, expected: any, msg: string?)
    if actual ~= expected then
        error((msg or "assertion failed") .. ": expected " .. tostring(expected) .. ", got " .. tostring(actual), 2)
    end
end

function M.neq(actual: any, expected: any, msg: string?)
    if actual == expected then
        error((msg or "assertion failed") .. ": expected not " .. tostring(expected), 2)
    end
end

function M.ok(val: any?, msg: string?): asserts val
    if not val then
        error((msg or "assertion failed") .. ": expected truthy value", 2)
    end
end

function M.fail(msg)
    error(msg or "assertion failed", 2)
end

function M.is_nil(val, msg)
    if val ~= nil then
        error((msg or "assertion failed") .. ": expected nil, got " .. tostring(val), 2)
    end
end

function M.not_nil(val: any?, msg: string?): asserts val
    if val == nil then
        error((msg or "assertion failed") .. ": expected non-nil value", 2)
    end
end

function M.is_string(val: any, msg: string?): asserts val is string
    if type(val) ~= "string" then
        error((msg or "assertion failed") .. ": expected string, got " .. type(val), 2)
    end
end

function M.is_number(val: any, msg: string?): asserts val is number
    if type(val) ~= "number" then
        error((msg or "assertion failed") .. ": expected number, got " .. type(val), 2)
    end
end

function M.is_table(val: any, msg: string?)
    if type(val) ~= "table" then
        error((msg or "assertion failed") .. ": expected table, got " .. type(val), 2)
    end
end

function M.is_function(val: any, msg: string?)
    if type(val) ~= "function" then
        error((msg or "assertion failed") .. ": expected function, got " .. type(val), 2)
    end
end

function M.is_boolean(val: any, msg: string?): asserts val is boolean
    if type(val) ~= "boolean" then
        error((msg or "assertion failed") .. ": expected boolean, got " .. type(val), 2)
    end
end

function M.contains(str, substr, msg)
    if type(str) ~= "string" or not string.find(str, substr, 1, true) then
        error((msg or "assertion failed") .. ": expected string to contain '" .. tostring(substr) .. "'", 2)
    end
end

function M.has_error(val, err, msg)
    if val ~= nil then
        error((msg or "has_error failed") .. ": expected nil result, got " .. tostring(val), 2)
    end
    if err == nil then
        error((msg or "has_error failed") .. ": expected error, got nil", 2)
    end
end

function M.no_error(val, err, msg)
    if err ~= nil then
        error((msg or "no_error failed") .. ": unexpected error: " .. tostring(err), 2)
    end
end

function M.throws(fn, msg)
    local ok, err = pcall(fn)
    if ok then
        error((msg or "throws failed") .. ": expected function to throw", 2)
    end
    return err
end

function M.not_throws(fn, msg)
    local ok, err = pcall(fn)
    if not ok then
        error((msg or "not_throws failed") .. ": unexpected error: " .. tostring(err), 2)
    end
end

-- Error structure assertions
function M.error_kind(err, expected_kind, msg)
    if err == nil then
        error((msg or "error_kind failed") .. ": error is nil", 2)
    end
    if type(err) ~= "table" then
        error((msg or "error_kind failed") .. ": error is not structured (got " .. type(err) .. ")", 2)
    end
    if err.kind ~= expected_kind then
        error((msg or "error_kind failed") .. ": expected kind '" .. tostring(expected_kind) .. "', got '" .. tostring(err.kind) .. "'", 2)
    end
end

function M.error_message(err, expected_msg, msg)
    if err == nil then
        error((msg or "error_message failed") .. ": error is nil", 2)
    end
    local actual_msg = type(err) == "table" and err.message or tostring(err)
    if actual_msg ~= expected_msg then
        error((msg or "error_message failed") .. ": expected message '" .. tostring(expected_msg) .. "', got '" .. tostring(actual_msg) .. "'", 2)
    end
end

function M.error_contains(err, substr, msg)
    if err == nil then
        error((msg or "error_contains failed") .. ": error is nil", 2)
    end
    local actual_msg = type(err) == "table" and err.message or tostring(err)
    if not string.find(actual_msg, substr, 1, true) then
        error((msg or "error_contains failed") .. ": expected error to contain '" .. tostring(substr) .. "', got '" .. tostring(actual_msg) .. "'", 2)
    end
end

return M
