local base64 = require("base64")

local function eq(actual, expected, msg)
    if actual ~= expected then
        error((msg or "assertion failed") .. ": expected " .. tostring(expected) .. ", got " .. tostring(actual), 2)
    end
end

local function main()
    -- Test encode basic
    eq(base64.encode("hello"), "aGVsbG8=", "encode hello")
    eq(base64.encode("world"), "d29ybGQ=", "encode world")
    eq(base64.encode(""), "", "encode empty string")
    eq(base64.encode("a"), "YQ==", "encode single char")
    eq(base64.encode("ab"), "YWI=", "encode two chars")
    eq(base64.encode("abc"), "YWJj", "encode three chars")

    -- Test decode basic
    eq(base64.decode("aGVsbG8="), "hello", "decode hello")
    eq(base64.decode("d29ybGQ="), "world", "decode world")
    eq(base64.decode(""), "", "decode empty string")
    eq(base64.decode("YQ=="), "a", "decode single char")
    eq(base64.decode("YWI="), "ab", "decode two chars")
    eq(base64.decode("YWJj"), "abc", "decode three chars")

    -- Test roundtrip
    local test_strings = {"", "a", "ab", "abc", "hello world", "binary\x00data\xff"}
    for _, s in ipairs(test_strings) do
        local encoded = base64.encode(s)
        local decoded = base64.decode(encoded)
        eq(decoded, s, "roundtrip for: " .. s)
    end

    -- Test encode error - invalid input type
    local result, err = base64.encode(123)
    if result ~= nil then error("expected nil result") end
    if err == nil then error("expected error") end
    if err:kind() ~= errors.INVALID then error("expected Invalid kind, got: " .. tostring(err:kind())) end
    if err:retryable() ~= false then error("expected retryable to be false") end

    -- Test decode error - invalid input type
    result, err = base64.decode(123)
    if result ~= nil then error("expected nil result") end
    if err == nil then error("expected error") end
    if err:kind() ~= errors.INVALID then error("expected Invalid kind") end
    if err:retryable() ~= false then error("expected retryable to be false") end

    -- Test decode error - invalid base64
    result, err = base64.decode("!!!invalid!!!")
    if result ~= nil then error("expected nil result") end
    if err == nil then error("expected error") end
    if err:kind() ~= errors.INVALID then error("expected Invalid kind") end
    if err:retryable() ~= false then error("expected retryable to be false") end
    local str = tostring(err)
    if not str or str == "" then error("error should have string representation") end
    if not string.find(str, "illegal base64", 1, true) then
        error("error message should contain 'illegal base64', got: " .. str)
    end

    return true
end

return { main = main }
