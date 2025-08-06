local env = require("env")
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

    local test_results = {
        memory_variables = {
            {
                operation = "Set & Verify Memory Test Variable",
                variable = "memory_test_env",
                expected_value = "new_actual_api_key",
                result = (env.set('memory_test_env', 'new_actual_api_key') and env.get('memory_test_env') == "new_actual_api_key") and "success" or "failed",
                stored_value = env.get('memory_test_env')
            },
            {
                operation = "Set & Verify Memory Test Variable by Full Name",
                variable = "app.env.demo:memory_test_env",
                expected_value = "new_actual_api_key_full",
                result = (env.set('app.env.demo:memory_test_env', 'new_actual_api_key_full') and env.get('app.env.demo:memory_test_env') == "new_actual_api_key_full") and "success" or "failed",
                stored_value = env.get('app.env.demo:memory_test_env')
            },
            {
                operation = "Set & Verify Memory Test Variable by ENV_NAME",
                variable = "MEMORY_TEST_ENV",
                expected_value = "new_actual_api_key_env",
                result = (env.set('MEMORY_TEST_ENV', 'new_actual_api_key_env') and env.get('MEMORY_TEST_ENV') == "new_actual_api_key_env") and "success" or "failed",
                stored_value = env.get('MEMORY_TEST_ENV')
            },
            {
                operation = "Set & Verify Memory Test Readonly Variable",
                variable = "memory_test_env_readonly",
                expected_value = "",
                result = (not env.set('memory_test_env_readonly', 'new_value') and env.get('memory_test_env_readonly') == "") and "success" or "failed",
                stored_value = env.get('memory_test_env_readonly')
            },
            {
                operation = "Set & Verify Memory Test Readonly Variable by Full Name",
                variable = "app.env.demo:memory_test_env_readonly",
                expected_value = "",
                result = (not env.set('app.env.demo:memory_test_env_readonly', 'new_value') and env.get('app.env.demo:memory_test_env_readonly') == "") and "success" or "failed",
                stored_value = env.get('app.env.demo:memory_test_env_readonly')
            },
            {
                operation = "Set & Verify Memory Test Readonly Variable by ENV_NAME",
                variable = "MEMORY_TEST_ENV_READONLY",
                expected_value = "",
                result = (not env.set('MEMORY_TEST_ENV_READONLY', 'new_value') and env.get('MEMORY_TEST_ENV_READONLY') == "") and "success" or "failed",
                stored_value = env.get('MEMORY_TEST_ENV_READONLY')
            },
        },
        file_variables = {
            {
                operation = "Set & Verify File Test Variable",
                variable = "file_test_env",
                expected_value = "new_file_value",
                result = (env.set('file_test_env', 'new_file_value') and env.get('file_test_env') == "new_file_value") and "success" or "failed",
                stored_value = env.get('file_test_env')
            },
            {
                operation = "Set & Verify File Test Variable by Full Name",
                variable = "app.env.demo:file_test_env",
                expected_value = "new_file_value_full",
                result = (env.set('app.env.demo:file_test_env', 'new_file_value_full') and env.get('app.env.demo:file_test_env') == "new_file_value_full") and "success" or "failed",
                stored_value = env.get('app.env.demo:file_test_env')
            },
            {
                operation = "Set & Verify File Test Variable by ENV_NAME",
                variable = "FILE_TEST_ENV",
                expected_value = "new_file_value_env",
                result = (env.set('FILE_TEST_ENV', 'new_file_value_env') and env.get('FILE_TEST_ENV') == "new_file_value_env") and "success" or "failed",
                stored_value = env.get('FILE_TEST_ENV')
            },
            {
                operation = "Set & Verify File Test Readonly Variable",
                variable = "file_test_env_readonly",
                expected_value = "file_value_readonly",
                result = (not env.set('file_test_env_readonly', 'new_value') and env.get('file_test_env_readonly') == "file_value_readonly") and "success" or "failed",
                stored_value = env.get('file_test_env_readonly')
            },
            {
                operation = "Set & Verify File Test Readonly Variable by Full Name",
                variable = "app.env.demo:file_test_env_readonly",
                expected_value = "file_value_readonly",
                result = (not env.set('app.env.demo:file_test_env_readonly', 'new_value') and env.get('app.env.demo:file_test_env_readonly') == "file_value_readonly") and "success" or "failed",
                stored_value = env.get('app.env.demo:file_test_env_readonly')
            },
            {
                operation = "Set & Verify File Test Readonly Variable by ENV_NAME",
                variable = "FILE_TEST_ENV_READONLY",
                expected_value = "file_value_readonly",
                result = (not env.set('FILE_TEST_ENV_READONLY', 'new_value') and env.get('FILE_TEST_ENV_READONLY') == "file_value_readonly") and "success" or "failed",
                stored_value = env.get('FILE_TEST_ENV_READONLY')
            },
        },
        os_variables = {
            {
                operation = "Get PATH Environment Variable",
                variable = "path_env",
                expected_value = "exists",
                result = (env.get('path_env') ~= nil and env.get('path_env') ~= "") and "success" or "failed",
                stored_value = env.get('path_env')
            },
            {
                operation = "Get PATH by Full Name",
                variable = "app.env.demo:path_env",
                expected_value = "exists",
                result = (env.get('app.env.demo:path_env') ~= nil and env.get('app.env.demo:path_env') ~= "") and "success" or "failed",
                stored_value = env.get('app.env.demo:path_env')
            },
            {
                operation = "Get HOME Environment Variable",
                variable = "home_env",
                expected_value = "exists",
                result = (env.get('home_env') ~= nil and env.get('home_env') ~= "") and "success" or "failed",
                stored_value = env.get('home_env')
            },
            {
                operation = "Get USER Environment Variable",
                variable = "user_env",
                expected_value = "exists",
                result = (env.get('user_env') ~= nil and env.get('user_env') ~= "") and "success" or "failed",
                stored_value = env.get('user_env')
            },
            {
                operation = "Get PWD Environment Variable",
                variable = "pwd_env",
                expected_value = "exists",
                result = (env.get('pwd_env') ~= nil and env.get('pwd_env') ~= "") and "success" or "failed",
                stored_value = env.get('pwd_env')
            },
            {
                operation = "Get SHELL Environment Variable",
                variable = "shell_env",
                expected_value = "exists",
                result = (env.get('shell_env') ~= nil and env.get('shell_env') ~= "") and "success" or "failed",
                stored_value = env.get('shell_env')
            },
            {
                operation = "Set PATH Variable (Should Fail)",
                variable = "path_env",
                expected_value = "readonly",
                result = (not env.set('path_env', 'new_path')) and "success" or "failed",
                stored_value = env.get('path_env')
            },
            {
                operation = "Get Non-existent OS Variable",
                variable = "NON_EXISTENT_VAR",
                expected_value = "nil",
                result = (env.get('NON_EXISTENT_VAR') == nil) and "success" or "failed",
                stored_value = env.get('NON_EXISTENT_VAR')
            },
        },
        app_env_variables = {
            {
                operation = "Get App HOME Environment Variable",
                variable = "app.env.demo:app_home_env",
                expected_value = "exists",
                result = (env.get('app.env.demo:app_home_env') ~= nil and env.get('app.env.demo:app_home_env') ~= "") and "success" or "failed",
                stored_value = env.get('app.env.demo:app_home_env')
            },
            {
                operation = "Set App HOME Variable (Should Succeed)",
                variable = "app.env.demo:app_home_env",
                expected_value = "writable",
                result = (env.set('app.env.demo:app_home_env', '/new/app/home/path') and env.get('app.env.demo:app_home_env') == "/new/app/home/path") and "success" or "failed",
                stored_value = env.get('app.env.demo:app_home_env')
            },
            {
                operation = "Get by Name",
                variable = "HOME",
                expected_value = "/new/app/home/path",
                result = (env.get('HOME') == "/new/app/home/path") and "success" or "failed",
                stored_value = env.get('HOME')
            },
        }
    }

    -- Calculate overall test statistics
    local total_tests = 0
    local passed_tests = 0
    for category_name, category in pairs(test_results) do
        if type(category) == "table" then
            for _, test in ipairs(category) do
                total_tests = total_tests + 1
                if test.result == "success" then
                    passed_tests = passed_tests + 1
                end
            end
        end
    end

    -- Create ASCII response
    local ascii_response = create_header("ENVIRONMENT TEST RESULTS") .. "\n"
    
    -- Add column headers
    ascii_response = ascii_response .. create_row("OPERATION", "VARIABLE", "EXPECTED", "STORED", "RESULT")
    ascii_response = ascii_response .. create_separator()

    -- Add memory variables section
    ascii_response = ascii_response .. create_header("MEMORY VARIABLES")
    for _, test in ipairs(test_results.memory_variables) do
        ascii_response = ascii_response .. create_row(test.operation, test.variable, test.expected_value, test.stored_value, test.result)
    end
    ascii_response = ascii_response .. create_separator()

    -- Add file variables section
    ascii_response = ascii_response .. create_header("FILE VARIABLES")
    for _, test in ipairs(test_results.file_variables) do
        ascii_response = ascii_response .. create_row(test.operation, test.variable, test.expected_value, test.stored_value, test.result)
    end
    ascii_response = ascii_response .. create_separator()

    -- Add OS variables section
    ascii_response = ascii_response .. create_header("OS VARIABLES (env.storage.os)")
    for _, test in ipairs(test_results.os_variables) do
        ascii_response = ascii_response .. create_row(test.operation, test.variable, test.expected_value, test.stored_value, test.result)
    end
    ascii_response = ascii_response .. create_separator()

    -- add app_env_variables section
    ascii_response = ascii_response .. create_header("APP ENV VARIABLES")
    for _, test in ipairs(test_results.app_env_variables) do
        ascii_response = ascii_response .. create_row(test.operation, test.variable, test.expected_value, test.stored_value, test.result)
    end
    ascii_response = ascii_response .. create_separator()

    -- Add summary
    ascii_response = ascii_response .. create_header("TEST SUMMARY")
    ascii_response = ascii_response .. "Total Tests: " .. total_tests .. "\n"
    ascii_response = ascii_response .. "Passed: " .. passed_tests .. "\n"
    ascii_response = ascii_response .. "Failed: " .. (total_tests - passed_tests) .. "\n"
    ascii_response = ascii_response .. "Success Rate: " .. string.format("%.2f%%", (passed_tests / total_tests) * 100) .. "\n"
    ascii_response = ascii_response .. string.rep("=", 114) .. "\n"

    -- Write the response and ensure it's sent
    local err = res:write(ascii_response)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write("Failed to send response: " .. tostring(err))
        return
    end

    -- Ensure the response is sent
    res:flush()
end

return {
    handler = handler
}