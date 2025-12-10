-- Worker that validates context values and errors if they don't match
local ctx = require("ctx")

local function main()
    -- Validate expected context values
    local request_id = ctx.get("request_id")
    if request_id ~= "req-123" then
        error("request_id mismatch: expected req-123, got " .. tostring(request_id))
    end

    local user_id = ctx.get("user_id")
    if user_id ~= 42 then
        error("user_id mismatch: expected 42, got " .. tostring(user_id))
    end

    local is_admin = ctx.get("is_admin")
    if is_admin ~= true then
        error("is_admin mismatch: expected true, got " .. tostring(is_admin))
    end

    return true
end

return { main = main }
