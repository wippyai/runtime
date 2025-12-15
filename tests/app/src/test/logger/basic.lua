local assert = require("assert2")
local logger = require("logger")

local function main()
    -- test debug level (colon syntax)
    logger:debug("debug message")

    -- test info level
    logger:info("info message")

    -- test warn level
    logger:warn("warn message")

    -- test error level
    logger:error("error message")

    -- test with fields
    logger:info("message with fields", {key = "value", count = 42})

    return true
end

return { main = main }
