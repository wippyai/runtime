-- process_data.lua
-- A minimal activity that processes simple input data

local json = require("json")

local function handler(input)
    -- Just log what we received
    print("Processing data with ID: " .. (input.id or "unknown"))

    -- Simple data transformation
    local result = {
        id = input.id,
        status = "processed",
        message = "Successfully processed data: " .. (input.name or "unnamed"),
        timestamp = os.time()
    }

    -- Return the processed result
    return result
end

return { handler = handler }