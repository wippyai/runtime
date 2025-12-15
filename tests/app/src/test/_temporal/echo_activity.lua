local ctx = require("ctx")

local input = ctx.payload()
if input == nil then
    return { message = "no input" }
end

return input
