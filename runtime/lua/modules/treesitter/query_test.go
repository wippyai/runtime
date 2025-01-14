package treesitter

import (
	"context"
	"os"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBasicQuery(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
		local treesitter = require("treesitter")
		
		-- Simple test code with a function
		local code = [[
			func hello() {
				println("world")
			}
		]]
		
		-- Parse the code
		local tree = treesitter.parse("go", code)
		assert(tree ~= nil, "tree should not be nil")
		
		local root = tree:root_node()
		assert(root ~= nil, "root should not be nil")
		
		-- Create a simple query to find the function
		local query = treesitter.query("go", "(function_declaration) @function")
		assert(query ~= nil, "query should not be nil")
		
		-- Execute query
		local matches = query:matches(root, code)
		assert(matches ~= nil, "matches should not be nil")
		
		-- Should find exactly one function
		local match_count = 0
		for _, match in pairs(matches) do
			match_count = match_count + 1
			
			-- Verify the match has captures
			assert(match.captures ~= nil, "match should have captures")
			assert(#match.captures > 0, "should have at least one capture")

			-- Verify the captured node
			local capture = match.captures[1]
			assert(capture.node ~= nil, "capture should have node")
			
			-- Get the text of the captured node
			local text = capture.node:text(code)

			assert(text:match("^func hello"), "captured text should start with 'func hello'")
		end
		
		assert(match_count == 1, "should find exactly one function")
	`, "test")
	assert.NoError(t, err)
}

func TestQueryMultipleCaptures(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
		local treesitter = require("treesitter")
		
		-- Test code with multiple functions and parameters
		local code = [[
func add(x int, y int) int {
	return x + y
}

func greet(name string) {
	println("Hello, " .. name)
}
]]
		
		-- Parse the code
		local tree = treesitter.parse("go", code)
		assert(tree ~= nil, "tree should not be nil")
		
		local root = tree:root_node()
		assert(root ~= nil, "root should not be nil")
		
		-- Create query to capture function name and parameters
		local query = treesitter.query("go", [[
(function_declaration
  name: (identifier) @func_name)
]])
		
		-- Execute query and debug output
		local matches = query:matches(root, code)
		assert(matches ~= nil, "matches should not be nil")
		
		-- Verify matches
		local found_add = false
		local found_greet = false
		
		for i, match in ipairs(matches) do
			assert(match.captures ~= nil, "match should have captures")
			
			for j, capture in ipairs(match.captures) do
				assert(capture.node ~= nil, "capture should have node")
				
				local text = capture.node:text(code)
				
				if text == "add" then
					found_add = true
				elseif text == "greet" then
					found_greet = true
				end
			end
		end
		
		assert(found_add, "should find 'add' function")
		assert(found_greet, "should find 'greet' function")
	`, "test")
	assert.NoError(t, err)
}

func TestQueryFunctionDetails(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
        local treesitter = require("treesitter")
        
        local code = [[
func add(x int, y int) int {
    return x + y
}

func greet(name string) string {
    return "Hello, " .. name
}

func log(msg string) {
    println(msg)
}
]]
        
        local tree = treesitter.parse("go", code)
        local root = tree:root_node()
        
        -- First get functions and their names
        local func_query = treesitter.query("go", [[
(function_declaration 
  name: (identifier) @func_name
  parameters: (parameter_list) @params
  result: (type_identifier)? @return_type)
]])

        local param_query = treesitter.query("go", [[
(parameter_list 
  (parameter_declaration
    name: (identifier) @param_name
    type: (type_identifier) @param_type))
]])

        local func_matches = func_query:matches(root, code)
        
        local functions = {}
        
        -- Process each function declaration
        for _, match in ipairs(func_matches) do
            local func = { name = nil, params = {}, return_type = nil }
            
            -- Find the function name and parameter list node
            for _, capture in ipairs(match.captures) do
                if capture.name == "func_name" then
                    func.name = capture.node:text(code)
                elseif capture.name == "return_type" then
                    func.return_type = capture.node:text(code)
                elseif capture.name == "params" then
                    -- Get parameters from the parameter list node
                    local param_matches = param_query:matches(capture.node, code)
                    for _, param_match in ipairs(param_matches) do
                        local param = {}
                        for _, param_capture in ipairs(param_match.captures) do
                            if param_capture.name == "param_name" then
                                param.name = param_capture.node:text(code)
                            elseif param_capture.name == "param_type" then
                                param.type = param_capture.node:text(code)
                            end
                        end
                        if param.name and param.type then
                            table.insert(func.params, param)
                        end
                    end
                end
            end
            table.insert(functions, func)
        end
        
        -- Verification phase
        for _, func in ipairs(functions) do
            
            -- Verify the function details
            if func.name == "add" then
                assert(#func.params == 2, "add should have 2 parameters")
                assert(func.params[1].name == "x", "first param should be x")
                assert(func.params[1].type == "int", "x should be int")
                assert(func.params[2].name == "y", "second param should be y")
                assert(func.params[2].type == "int", "y should be int")
                assert(func.return_type == "int", "add should return int")
            elseif func.name == "greet" then
                assert(#func.params == 1, "greet should have 1 parameter")
                assert(func.params[1].name == "name", "param should be name")
                assert(func.params[1].type == "string", "name should be string")
                assert(func.return_type == "string", "greet should return string")
            elseif func.name == "log" then
                assert(#func.params == 1, "log should have 1 parameter")
                assert(func.params[1].name == "msg", "param should be msg")
                assert(func.params[1].type == "string", "msg should be string")
                assert(func.return_type == nil, "log should not have return type")
            else
                error("Unexpected function: " .. func.name)
            end
        end
    `, "test")
	assert.NoError(t, err)
}

func TestQueryParamDebug(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
        local treesitter = require("treesitter")

local code = [[
func add(x int, y int) int {
	return x + y
}

func greet(name string) string {
	return "Hello, " .. name
}

func log(msg string) {
	println(msg)
}
]]

-- Print tree structure for debugging
local tree = treesitter.parse("go", code)
local root = tree:root_node()

-- First get function declarations with their parameter lists
local func_query = treesitter.query("go", [[
(function_declaration
  name: (identifier) @func_name
  parameters: (parameter_list) @params
  result: (type_identifier)? @return_type)
]])

-- Query for parameters within a parameter list
local param_query = treesitter.query("go", [[
(parameter_list
  (parameter_declaration 
    name: (identifier) @param_name 
    type: (type_identifier) @param_type))
]])

local func_matches = func_query:matches(root, code)

local functions = {}

-- Process function matches
for i, match in ipairs(func_matches) do
    
    local func = { name = nil, params = {}, return_type = nil }
    
    for j, capture in ipairs(match.captures) do
        local text = capture.node:text(code)
        
        if capture.name == "func_name" then
            func.name = text
        elseif capture.name == "return_type" then
            func.return_type = text
        elseif capture.name == "params" then
            local param_matches = param_query:matches(capture.node, code)
            
            -- Process parameter matches
            for k, param_match in ipairs(param_matches) do
                local param = {}
                
                for l, param_capture in ipairs(param_match.captures) do
                    local param_text = param_capture.node:text(code)
                    
                    if param_capture.name == "param_name" then
                        param.name = param_text
                    elseif param_capture.name == "param_type" then
                        param.type = param_text
                    end
                end
                
                if param.name and param.type then
                    table.insert(func.params, param)
                end
            end
        end
    end
    
    table.insert(functions, func)
end

-- Verification phase
for _, func in ipairs(functions) do
    
    -- Assertions
    if func.name == "add" then
        assert(#func.params == 2, "add should have 2 parameters")
        assert(func.params[1].name == "x", "first param should be x")
        assert(func.params[1].type == "int", "x should be int")
        assert(func.params[2].name == "y", "second param should be y")
        assert(func.params[2].type == "int", "y should be int")
        assert(func.return_type == "int", "add should return int")
    elseif func.name == "greet" then
        assert(#func.params == 1, "greet should have 1 parameter")
        assert(func.params[1].name == "name", "param should be name")
        assert(func.params[1].type == "string", "name should be string")
        assert(func.return_type == "string", "greet should return string")
    elseif func.name == "log" then
        assert(#func.params == 1, "log should have 1 parameter")
        assert(func.params[1].name == "msg", "param should be msg")
        assert(func.params[1].type == "string", "msg should be string")
        assert(func.return_type == nil, "log should not have return type")
    else
        error("Unexpected function: " .. func.name)
    end
end
    `, "test")
	assert.NoError(t, err)
}

func TestQueryAdvancedFeatures(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
        local treesitter = require("treesitter")
        
        local code = [[
func example(x int, y string) int {
    if x > 0 {
        return x
    }
    println(y)
    return 0
}
]]
        
        local tree = treesitter.parse("go", code)
        local root = tree:root_node()
        
        -- Create query with multiple patterns
        local query = treesitter.query("go", [[
          (function_declaration) @func
          (parameter_declaration name: (identifier) @param_name type: (type_identifier) @param_type) 
          (if_statement condition: (binary_expression) @condition)
        ]])

        -- Test pattern count and capture count
        local pattern_count = query:pattern_count()
        assert(pattern_count == 3, "should have 3 patterns")
        
        local capture_count = query:capture_count()
        assert(capture_count == 4, "should have 4 captures") -- func, param_name, param_type, condition
        
        -- Get all capture names
        local capture_names = {}
        for i = 0, capture_count-1 do
            local name = query:capture_name_for_id(i)
            capture_names[name] = true
        end

        -- Verify we have all expected capture names
        assert(capture_names["func"], "should have func capture")
        assert(capture_names["param_name"], "should have param_name capture")
        assert(capture_names["param_type"], "should have param_type capture")
        assert(capture_names["condition"], "should have condition capture")
        
        -- Test match limit and timeout
        query:set_match_limit(1000)
        local limit = query:get_match_limit()
        assert(limit == 1000, "match limit should be set")
        
        query:set_timeout(5000)
        local timeout = query:get_timeout()
        assert(timeout == 5000, "timeout should be set")

        -- Test byte and point range
        query:set_byte_range(0, string.len(code))
        query:set_point_range({row=0, column=0}, {row=10, column=0})

        -- Test matches
        local matches = query:matches(root, code)
        
        local found_func = false
        local found_param = false
        local found_condition = false

        for i, match in ipairs(matches) do
            for j, capture in ipairs(match.captures) do
                local text = capture.node:text(code)
                if capture.name == "func" then
                    found_func = true
                elseif capture.name == "param_name" then
                    found_param = true
                    assert(text == "x" or text == "y", "param should be x or y")
                elseif capture.name == "condition" then
                    found_condition = true
                    assert(text:find("x > 0"), "condition should contain x > 0")
                end
            end
        end

        -- Test captures API
        local captures = query:captures(root, code)
        -- In Lua, table with a metatable can define __call behavior
        -- Check if captures are non-nil and no error occurred
        assert(captures ~= nil, "captures should not be nil")

        -- Test property settings and predicates
        local predicates = query:get_property_predicates(0)
        assert(predicates ~= nil, "predicates should not be nil")

        local settings = query:get_property_settings(0)
        assert(settings ~= nil, "settings should not be nil")

        -- Final verification
        assert(found_func, "should find function declaration")
        assert(found_param, "should find parameters")
        assert(found_condition, "should find if condition")

    `, "test")
	assert.NoError(t, err)
}

func TestQueryOperations(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
		local treesitter = require("treesitter")

-- Test code with rich syntax for comprehensive query testing
local code = [[
func process(data string) error {
    if len(data) == 0 {
        return fmt.Errorf("empty data")
    }
    if !isValid(data) {
        return nil
    }
    for i := 0; i < len(data); i++ {
        handleChar(data[i])
    }
    return nil
}

func isValid(s string) bool {
    return len(s) > 5
}

func handleChar(c byte) {
    if c >= '0' && c <= '9' {
        processDigit(c - '0')
    }
}
]]

local tree = treesitter.parse("go", code)
local root = tree:root_node()

-- Test query creation and error handling
local query = treesitter.query("go", [[
    (function_declaration
        name: (identifier) @func_name
        parameters: (parameter_list) @params
        result: [(type_identifier) (ERROR)]? @return_type) @function

    (if_statement 
        condition: (_) @if_condition) @if
    
    (binary_expression
        left: (_) @left
        operator: (_) @op
        right: (_) @right) @binary
    
    ((identifier) @id
     (#match? @id "^process"))
]])

assert(query ~= nil, "query should not be nil")

-- Test queryDidExceedMatchLimit
query:set_match_limit(1)
local matches = query:matches(root, code)
local exceeded = query:did_exceed_match_limit()
assert(exceeded, "should exceed match limit when set to 1")

-- Test queryDisablePattern and queryIsPatternRooted
query:disable_pattern(0)
local is_rooted = query:is_pattern_rooted(1)
assert(is_rooted ~= nil, "is_pattern_rooted should return a value")

-- Test queryDisableCapture
query:disable_capture("func_name")

-- Test queryIsPatternNonLocal
local is_non_local = query:is_pattern_non_local(0)
assert(is_non_local ~= nil, "is_pattern_non_local should return a value")

-- Test queryCaptureNameForId and queryCaptureQuantifier
local capture_name = query:capture_name_for_id(0)
assert(capture_name ~= nil, "should get capture name")
local quantifier = query:capture_quantifier(0, 0)
assert(quantifier ~= nil, "should get capture quantifier")

-- Test queryStringCount and queryStartByteForPattern
local string_count = query:string_count()
assert(string_count ~= nil, "should get string count")
local start_byte = query:start_byte_for_pattern(0)
assert(start_byte ~= nil, "should get start byte")

-- Test querySetMaxStartDepth
query:set_max_start_depth(5)

-- Test queryGetPropertyPredicates with pattern validation
local predicates = query:get_property_predicates(0)
assert(predicates ~= nil, "should get property predicates")
for _, pred in ipairs(predicates) do
    assert(pred.key ~= nil, "predicate should have key")
end

-- Test queryGetPropertySettings with settings validation
local settings = query:get_property_settings(0)
assert(settings ~= nil, "should get property settings")
for _, setting in ipairs(settings) do
    assert(setting.key ~= nil, "setting should have key")
end

-- Test queryIsPatternGuaranteed
local is_guaranteed = query:is_pattern_guaranteed(0)
assert(type(is_guaranteed) == "boolean", "should return boolean for pattern guarantee")

-- Test queryCaptureIndexForName
local capture_index = query:capture_index_for_name("func_name")
assert(capture_index ~= nil, "should get capture index")

-- Test queryEndByteForPattern
local end_byte = query:end_byte_for_pattern(0)
assert(end_byte ~= nil, "should get end byte")

-- Test queryGetTextPredicates
local text_predicates = query:get_text_predicates(0)
assert(text_predicates ~= nil, "should get text predicates")

-- Test error handling
local bad_query, err = treesitter.query("go", "((invalid query")
assert(bad_query == nil, "invalid query should return nil")
assert(err ~= nil, "invalid query should return error message")

-- Test garbage collection
query = nil
collectgarbage("collect")
	`, "test")

	assert.NoError(t, err)
}

func TestQueryErrorCases(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
		local treesitter = require("treesitter")

-- Test different query error types
local error_cases = {
    -- Syntax error
    {
        name = "syntax error - unmatched parenthesis",
        query = "(()",
        expected = "Query error at 1:3%. Invalid syntax:"
    },
    -- Node type error
    {
        name = "invalid node type",
        query = "(nonexistent_node)",
        expected = "Query error at 1:%d+%. Invalid node type"
    },
    -- Capture syntax error
    {
        name = "invalid capture syntax",
        query = "(identifier @)",
        expected = "Query error at 1:%d+%. Invalid"
    },
    -- Structure error
    {
        name = "invalid structure",
        query = "((identifier) ()",
        expected = "Query error at"
    }
}

for _, case in ipairs(error_cases) do
    local query, err = treesitter.query("go", case.query)
    
    -- Should not create a valid query
    assert(query == nil, "invalid query '" .. case.query .. "' should return nil")
    
    -- Should have an error message
    assert(err ~= nil, "should have error message for query: " .. case.query)
    
    -- Error message should match expected pattern
    assert(err:match(case.expected), 
           string.format("\nError message did not match pattern.\nGot: '%s'\nExpected to match: '%s'", 
                        err, case.expected))
end

-- Test valid queries
local valid_queries = {
    "(function_declaration) @func",
    "((identifier) @id (#match? @id \"^[A-Z]\"))"
}

for _, query_str in ipairs(valid_queries) do
    local query = treesitter.query("go", query_str)
    assert(query ~= nil, "valid query should not return nil: " .. query_str)
end
	`, "test")

	assert.NoError(t, err)
}

// todo: all this tests should be migrated to internal engine later
func TestQueryTextPredicates(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
		local treesitter = require("treesitter")

-- Test code with various matches for predicates
local code = [[
func ProcessData(data string) error {
    var result int
    if isValid(data) {
        result = calculate(data)
    }
    return validateResult(result)
}

type Handler struct {
    Name string
    ID   int
}

func (h *Handler) Process() {
    fmt.Println("Processing with:", h.Name)
}
]]

-- Create query with various predicates
local query = treesitter.query("go", [[
(identifier) @id
  (#match? @id "^Process")

(function_declaration
  name: (identifier) @func
  (#match? @func "^[A-Z]"))

(field_declaration 
  name: (field_identifier) @field
  type: (type_identifier) @type
  (#eq? @type "string"))

(type_identifier) @type
  (#eq? @type "Handler")
]])

assert(query ~= nil, "query creation failed")

-- Get text predicates for each pattern
for i = 0, query:pattern_count() - 1 do
    local predicates = query:get_text_predicates(i)
    assert(predicates ~= nil, "should get text predicates for pattern " .. i)
    for j, pred in ipairs(predicates) do
    end
end

-- Get property predicates
for i = 0, query:pattern_count() - 1 do
    local predicates = query:get_property_predicates(i)
    assert(predicates ~= nil, "should get property predicates for pattern " .. i)
    for j, pred in ipairs(predicates) do
    end
end

-- Execute query and validate matches
local root = treesitter.parse("go", code):root_node()
local matches = query:matches(root, code)

for i, match in ipairs(matches) do
    for j, capture in ipairs(match.captures) do
        local text = capture.node:text(code)
    end
end

-- Test capture quantifiers
for i = 0, query:pattern_count() - 1 do
    for j = 0, query:capture_count() - 1 do
        local quantifier = query:capture_quantifier(i, j)
    end
end

query:close()
	`, "test")

	assert.NoError(t, err)
}

func TestQueryNestedGrammars(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	file := `scripts/nested_grammars.lua`

	code, err := os.ReadFile(file)
	require.NoError(t, err)

	err = vm.DoString(context.Background(), string(code), "test")
	assert.NoError(t, err)
}

func TestQueryLuaInLua(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	file := `scripts/lua_in.lua`

	code, err := os.ReadFile(file)
	require.NoError(t, err)

	err = vm.DoString(context.Background(), string(code), "test")
	assert.NoError(t, err)
}

func TestQueryLuaFileStruct(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	file := `scripts/lua_file_func.lua`

	code, err := os.ReadFile(file)
	require.NoError(t, err)

	err = vm.DoString(context.Background(), string(code), "test")
	assert.NoError(t, err)
}
