local text = require("text")

local function test_regexp_match(): boolean
    local regex, err = text.regexp.compile("hello")
    if err ~= nil then return false end
    local result: boolean = regex:match_string("hello world")
    return result == true
end

local function test_regexp_find(): boolean
    local regex, err = text.regexp.compile("\\d+")
    if err ~= nil then return false end
    local result: string? = regex:find_string("abc123def")
    return result == "123"
end

local function test_regexp_split(): boolean
    local regex, err = text.regexp.compile(",")
    if err ~= nil then return false end
    local parts = regex:split("a,b,c", -1)
    return #parts == 3 and parts[1] == "a" and parts[2] == "b" and parts[3] == "c"
end

local function test_regexp_replace(): boolean
    local regex, err = text.regexp.compile("world")
    if err ~= nil then return false end
    local result: string = regex:replace_all_string("hello world", "lua")
    return result == "hello lua"
end

local function main(): boolean
    return test_regexp_match() and test_regexp_find() and test_regexp_split() and test_regexp_replace()
end

return { main = main }
