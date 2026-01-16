local io = require("io")

local function main()
    io.print("Hello from CLI!")
    return 0
end

return { main = main }
