local env = require("env")
local http = require("http")

-- Helper function to create ASCII table header
local function create_header(title)
    local width = 100
    local padding = math.floor((width - #title) / 2)
    return string.rep("=", width) .. "\n" ..
           string.rep(" ", padding) .. title .. "\n" ..
           string.rep("=", width) .. "\n"
end

-- Helper function to create ASCII table row
local function create_row(operation, variable, expected)
    local op_width = 50
    local var_width = 30
    local val_width = 20
    
    -- Format and pad values
    local op = (operation or ""):sub(1, op_width) .. string.rep(" ", op_width - #operation)
    local var = (variable or ""):sub(1, var_width) .. string.rep(" ", var_width - #variable)
    local exp = tostring(expected or "nil"):sub(1, val_width) .. string.rep(" ", val_width - #tostring(expected or "nil"))
    
    return "| " .. op .. " | " .. var .. " | " .. exp .. " |\n"
end

-- Main handler function
local function handler()
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    res:set_content_type(http.CONTENT.TEXT)
    res:set_status(http.STATUS.OK)

    -- Test environment variables defined with YAML anchors
    local tests = {
        {
            operation = "Get Database Host (YAML Anchor)",
            variable = "DATABASE_HOST",
            expected = "localhost"
        },
        {
            operation = "Get Database Port (YAML Anchor)",
            variable = "DATABASE_PORT",
            expected = "5432"
        }
    }

    -- Build response
    local response = create_header("DEPENDENCIES DEMO - YAML ANCHORS TEST") .. "\n"
    response = response .. create_row("OPERATION", "VARIABLE", "EXPECTED")
    response = response .. string.rep("-", 100) .. "\n"

    -- Add test results
    local passed = 0
    for _, test in ipairs(tests) do
        local actual = env.get(test.variable)
        local result = (actual == test.expected) and "success" or "failed"
        if result == "success" then passed = passed + 1 end
        response = response .. create_row(test.operation, test.variable, test.expected)
    end

    -- Add summary
    response = response .. string.rep("-", 100) .. "\n"
    response = response .. create_header("SUMMARY")
    response = response .. string.format("Passed: %d/%d (%.1f%%)\n", passed, #tests, (passed/#tests)*100)
    response = response .. string.rep("=", 100) .. "\n"

    -- Add explanation
    response = response .. create_header("YAML ANCHOR EXPLANATION")
    response = response .. "env_template: &env_template - Defines reusable template\n"
    response = response .. "<<: *env_template - Merges template into entries\n"
    response = response .. "Reduces duplication and ensures consistency!\n"
    response = response .. string.rep("=", 100) .. "\n"

    res:write(response)
    res:flush()
end

return { handler = handler } 