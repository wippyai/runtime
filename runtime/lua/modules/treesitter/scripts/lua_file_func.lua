-- Load treesitter module
local ts = require 'treesitter'

-- Create reusable parser
local parser = ts.parser()
assert(parser:set_language('lua'), 'Failed to set Lua language')

-- Create reusable query
local query = ts.query('lua', [[
    (function_declaration) @function
    (return_statement) @return
]])

local function test_single_function_no_return(code)
    -- Reset parser and parse code
    parser:reset()
    local tree = parser:parse(code)
    local root = tree:root_node()

    -- Count function declarations and return statements
    local function_count = 0
    local return_count = 0

    local matches = query:matches(root, code)
    for _, match in ipairs(matches) do
        for _, capture in ipairs(match.captures) do
            if capture.name == 'function' then
                function_count = function_count + 1
            elseif capture.name == 'return' then
                return_count = return_count + 1
            end
        end
    end

    -- Return validation results
    return {
        valid = function_count == 1 and return_count == 0,
        errors = {
            function_count = function_count ~= 1 and
                string.format('Expected 1 function, found %d', function_count) or nil,
            return_statement = return_count > 0 and
                string.format('Found %d return statements, expected none', return_count) or nil
        }
    }
end

-- Example usage and tests:
local test_code = [[
function processData(input)
    local result = input * 2
    print(result)
end
]]

local result = test_single_function_no_return(test_code)
assert(result.valid, 'Test should pass for valid code')

-- Test invalid code with multiple functions
local invalid_code_1 = [[
function foo() end
function bar() end
]]

local result1 = test_single_function_no_return(invalid_code_1)
assert(not result1.valid, 'Test should fail for multiple functions')
assert(result1.errors.function_count == 'Expected 1 function, found 2',
    'Should report correct number of functions')

-- Test invalid code with return statement
local invalid_code_2 = [[
function process()
    local x = 1
    return x
end
]]

local result2 = test_single_function_no_return(invalid_code_2)
assert(not result2.valid, 'Test should fail for code with return statement')
assert(result2.errors.return_statement == 'Found 1 return statements, expected none',
    'Should report return statement')

return {
    test = test_single_function_no_return,
    cleanup = function()
        parser:close()
        query:close()
    end
}
