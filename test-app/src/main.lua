local strings = require("wippyai.dd3:strings")

local function main()
    print("Testing strings library from hub:")

    local text = "  hello world  "
    print("Original: '" .. text .. "'")
    print("Trimmed: '" .. strings.trim(text) .. "'")
    print("Capitalized: '" .. strings.capitalize(strings.trim(text)) .. "'")
    print("Reversed: '" .. strings.reverse(strings.trim(text)) .. "'")

    local parts = strings.split("one,two,three", ",")
    print("Split 'one,two,three': " .. table.concat(parts, " | "))

    print("Starts with 'hello': " .. tostring(strings.starts_with("hello world", "hello")))
    print("Ends with 'world': " .. tostring(strings.ends_with("hello world", "world")))
end

return { main = main }
