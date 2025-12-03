-- Test: text.regexp functionality
local assert = require("assert2")

local function main()
    local text = require("text")

    -- Test compile success
    local re, err = text.regexp.compile("[a-z]+")
    assert.is_nil(err, "compile should not error")
    assert.not_nil(re, "regexp object returned")

    -- Test compile error
    local bad_re, bad_err = text.regexp.compile("[invalid")
    assert.is_nil(bad_re, "invalid pattern returns nil")
    assert.not_nil(bad_err, "invalid pattern returns error")
    assert.eq(bad_err:kind(), errors.INVALID, "error kind is INVALID")
    assert.eq(bad_err:retryable(), false, "error is not retryable")

    -- Test match_string
    local num_re, _ = text.regexp.compile("[0-9]+")
    assert.eq(num_re:match_string("hello123"), true, "matches digits")
    assert.eq(num_re:match_string("hello"), false, "no match without digits")

    -- Test find_string
    local match = num_re:find_string("hello123world456")
    assert.eq(match, "123", "finds first match")
    assert.is_nil(num_re:find_string("hello"), "nil when no match")

    -- Test find_all_string
    local matches = num_re:find_all_string("a1b2c3")
    assert.eq(#matches, 3, "finds all matches")
    assert.eq(matches[1], "1", "first match")
    assert.eq(matches[2], "2", "second match")
    assert.eq(matches[3], "3", "third match")

    -- Test find_string_submatch
    local group_re, _ = text.regexp.compile("([a-z]+)([0-9]+)")
    local submatch = group_re:find_string_submatch("hello123world")
    assert.not_nil(submatch, "submatch found")
    assert.eq(#submatch, 3, "full match + 2 groups")
    assert.eq(submatch[1], "hello123", "full match")
    assert.eq(submatch[2], "hello", "first group")
    assert.eq(submatch[3], "123", "second group")

    -- Test find_all_string_submatch
    local email_re, _ = text.regexp.compile("([a-z]+)@([a-z]+)")
    local all_submatches = email_re:find_all_string_submatch("a@b c@d")
    assert.eq(#all_submatches, 2, "two email matches")
    assert.eq(all_submatches[1][1], "a@b", "first full match")
    assert.eq(all_submatches[2][2], "c", "second username")

    -- Test replace_all_string
    local result = num_re:replace_all_string("a1b2c3", "X")
    assert.eq(result, "aXbXcX", "replaces all matches")

    -- Test split
    local comma_re, _ = text.regexp.compile(",")
    local parts = comma_re:split("a,b,c")
    assert.eq(#parts, 3, "splits into 3 parts")
    assert.eq(parts[1], "a", "first part")
    assert.eq(parts[2], "b", "second part")
    assert.eq(parts[3], "c", "third part")

    -- Test split with limit
    local limited = comma_re:split("a,b,c,d", 2)
    assert.eq(#limited, 2, "limited to 2 parts")
    assert.eq(limited[2], "b,c,d", "remainder in second part")

    -- Test num_subexp
    assert.eq(group_re:num_subexp(), 2, "2 capture groups")
    assert.eq(num_re:num_subexp(), 0, "no capture groups")

    -- Test subexp_names
    local named_re, _ = text.regexp.compile("(?P<name>[a-z]+)(?P<num>[0-9]+)")
    local names = named_re:subexp_names()
    assert.eq(#names, 3, "3 names including full match")
    assert.eq(names[1], "", "full match is unnamed")
    assert.eq(names[2], "name", "first group name")
    assert.eq(names[3], "num", "second group name")

    -- Test string
    assert.eq(num_re:string(), "[0-9]+", "returns original pattern")

    -- Test find_string_index
    local page_re, _ = text.regexp.compile("page\\d+")
    local content = "The page1 here"
    local idx = page_re:find_string_index(content)
    assert.not_nil(idx, "index found")
    assert.eq(idx[1], 5, "start index (1-based)")
    local extracted = content:sub(idx[1], idx[2])
    assert.eq(extracted, "page1", "extracted substring")

    return true
end

return { main = main }
