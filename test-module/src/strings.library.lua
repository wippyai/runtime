-- String utility functions

local M = {}

-- Reverses a string
function M.reverse(s)
    return string.reverse(s)
end

-- Capitalizes the first letter
function M.capitalize(s)
    if #s == 0 then return s end
    return string.upper(string.sub(s, 1, 1)) .. string.sub(s, 2)
end

-- Checks if string starts with prefix
function M.starts_with(s, prefix)
    return string.sub(s, 1, #prefix) == prefix
end

-- Checks if string ends with suffix
function M.ends_with(s, suffix)
    return string.sub(s, -#suffix) == suffix
end

-- Trims whitespace from both ends
function M.trim(s)
    return string.match(s, "^%s*(.-)%s*$")
end

-- Splits string by delimiter
function M.split(s, delimiter)
    local result = {}
    local pattern = "([^" .. delimiter .. "]+)"
    for match in string.gmatch(s, pattern) do
        table.insert(result, match)
    end
    return result
end

return M
