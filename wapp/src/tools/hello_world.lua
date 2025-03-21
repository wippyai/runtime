local M = {}

-- Simple hello world function
-- Takes a name parameter and returns a greeting
function M.greet(params)
    local name = params.name or "World"
    return {
        message = "Hello, " .. name .. "!",
        timestamp = os.time()
    }
end

return M
