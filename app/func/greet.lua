local M = {}

function M.greet(name)
    name = name or "World"
    return string.format("Hello, %s!", name)
end

return M
