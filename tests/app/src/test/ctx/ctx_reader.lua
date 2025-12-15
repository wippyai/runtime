-- Helper function that reads context values and returns them
local ctx = require("ctx")

local function main(keys)
    local result = {}

    if type(keys) == "table" then
        for _, key in ipairs(keys) do
            local val, err = ctx.get(key)
            if not err then
                result[key] = val
            end
        end
    elseif type(keys) == "string" then
        local val, err = ctx.get(keys)
        if not err then
            result[keys] = val
        end
    end

    return result
end

return { main = main }
