local http = require("http")

-- Helper function to create ASCII table header
local function create_header(title)
    local width = 114  -- Increased width to match new table width
    local padding = math.floor((width - #title) / 2)
    return string.rep("=", width) .. "\n" ..
           string.rep(" ", padding) .. title .. "\n" ..
           string.rep("=", width) .. "\n"
end

-- Helper function to create ASCII table row
local function create_row(operation, variable, expected, stored, result)
    local op_width = 30    -- Increased from 25
    local var_width = 35   -- Increased from 20
    local val_width = 25   -- Increased from 15
    local res_width = 10
    
    -- Format the values
    local op = operation:sub(1, op_width)
    local var = (variable or ""):sub(1, var_width)
    local exp = tostring(expected or "nil"):sub(1, val_width)
    local str = tostring(stored or "nil"):sub(1, val_width)
    local res = tostring(result):sub(1, res_width)
    
    -- Pad the values
    op = op .. string.rep(" ", op_width - #op)
    var = var .. string.rep(" ", var_width - #var)
    exp = exp .. string.rep(" ", val_width - #exp)
    str = str .. string.rep(" ", val_width - #str)
    res = res .. string.rep(" ", res_width - #res)
    
    return "| " .. op .. " | " .. var .. " | " .. exp .. " | " .. str .. " | " .. res .. " |\n"
end

-- Helper function to create ASCII table separator
local function create_separator()
    return string.rep("-", 114) .. "\n"  -- Increased width to match new table width
end

-- Main handler function
local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Set up response headers
    res:set_content_type(http.CONTENT.TEXT)
    res:set_status(http.STATUS.OK)

    -- Ensure the response is sent
    res:flush()
end

return {
    handler = handler
}