# Lua Test Framework Documentation

## Overview

This test framework provides a BDD-style testing solution for Lua applications. It includes support for test suites, individual test cases, various assertion types, lifecycle hooks, and a powerful mocking system.

## Getting Started

### Basic Test Structure

```lua
-- Import the test framework
local test = require("test")

-- Define your test cases
local function define_tests()
    describe("My Test Suite", function()
        it("should perform a basic test", function()
            local result = 1 + 1
            expect(result).to_equal(2)
        end)
        
        it("should handle another case", function()
            expect("hello").to_be_type("string")
        end)
    end)
end

-- Run the tests
return test.run_cases(define_tests)
```

### Running Tests

To run tests, call `test.run_cases(define_tests_fn)` with a function that defines your tests. This returns a function that can be called with options:

```lua
local runner = test.run_cases(define_tests)
local results = runner({
    pid = process.pid,  -- Process ID for messaging
    ref_id = "my-test", -- Optional reference ID
    topic = "test:update" -- Optional custom topic
})
```

## Writing Tests

### Test Suites and Cases

```lua
describe("Suite Name", function()
    -- Test cases go here
    it("should do something", function()
        -- Test logic
    end)
    
    it("should do something else", function()
        -- More test logic
    end)
    
    -- Skip a test
    it_skip("not ready yet", function()
        -- This test will be skipped
    end)
end)
```

### Assertions

The framework provides many assertion methods through the `expect` function:

```lua
-- Basic equality
expect(value).to_equal(expected)
expect(value).not_to_equal(unexpected)

-- Truth testing
expect(value).to_be_true()
expect(value).to_be_false()

-- Nil checks
expect(value).to_be_nil()
expect(value).not_to_be_nil()

-- Type checking
expect(value).to_be_type("string")

-- String pattern matching
expect("test string").to_match("^test")

-- Table assertions
expect(table).to_contain(expected_value)
expect(table).to_have_key(key_name)
```

## Lifecycle Hooks

You can define hooks that run before or after tests:

```lua
describe("Suite with hooks", function()
    before_all(function()
        -- Runs once before all tests in this suite
        setup_database()
    end)
    
    after_all(function()
        -- Runs once after all tests in this suite
        cleanup_database()
    end)
    
    before_each(function()
        -- Runs before each test
        reset_state()
    end)
    
    after_each(function()
        -- Runs after each test
        clear_cache()
    end)
    
    it("test with hooks", function()
        -- Test code
    end)
end)
```

## Mocking System

The framework includes a powerful mocking system for replacing functions during tests.

### Basic Mocking

```lua
-- Mock a function on an object
mock(object, "method_name", function(...)
    -- Replacement implementation
    return mock_result
end)

-- Mock using a string path (for global objects)
mock("process.send", function(pid, topic, payload)
    -- Replacement implementation
    return true
end)

-- Restore a specific mock
restore_mock(object, "method_name")
-- Or by string path
restore_mock("process.send")

-- Restore all mocks (done automatically at the end of each test)
restore_all_mocks()
```

### Process Mocking

The framework has special support for mocking the `process` object:

```lua
-- Mock process.send
mock_process("send", function(pid, topic, payload)
    -- Replacement implementation
    return true
end)

-- Create and mock process object if it doesn't exist
mock_process()
```

### Tracking Mock Calls

A common pattern is to track calls to a mocked function:

```lua
it("should call the right function", function()
    local calls = {}
    
    mock(object, "method", function(arg1, arg2)
        table.insert(calls, {arg1, arg2})
        return true
    end)
    
    -- Call code that should use the mocked function
    some_function()
    
    -- Verify the mock was called with expected arguments
    expect(#calls).to_equal(1)
    expect(calls[1][1]).to_equal("expected_arg1")
end)
```

## Advanced Features

### Mock Namespaces

For better mock debugging, register namespaces for tables:

```lua
test.register_mock_namespace(my_table, "my_table")
```

### Test Framework Integration

The framework can integrate with a test runner using process messaging:

```lua
test.setup_process_integration({
    pid = parent_pid,
    ref_id = "test-run-123",
    topic = "test:results"
})
```

### Error Reporting

Report test errors manually:

```lua
test.report_error("Something went wrong", "context")
```

## Aliased Methods

For different style preferences, some methods have aliases:

```lua
-- Instead of describe
test.spec("My specs", function() end)
test.context("My context", function() end)

-- Instead of expect
test.assert(value).to_equal(expected)
```

## Best Practices

1. Keep tests focused and simple
2. Restore mocks after using them (done automatically if using hooks)
3. Use descriptive suite and test names
4. Group related tests in the same suite
5. Use lifecycle hooks to avoid repetitive setup/teardown code
6. Be careful when mocking process.send as it's used by the test framework

## Common Patterns

### Testing async code

```lua
it("should handle async code", function()
    local ch = channel.new()
    local timeout_ch = time.after("1000ms")  -- 1 second timeout
    
    -- Spawn a coroutine to run the async operation
    coroutine.spawn(function()
        local result = async_function()
        ch:send(result)
    end)
    
    -- Wait for the result with a timeout
    local result = channel.select{
        ch:case_receive(),
        timeout_ch:case_receive()
    }
    
    if result.channel == timeout_ch then
        error("Timeout waiting for async operation")
    else
        expect(result.value).to_equal("expected")
    end
end)
```

Alternative approach using cpcall:

```lua
it("should handle async operation with cpcall", function()
    -- cpcall allows async operations and returns result or error
    local success, result = cpcall(function()
        return async_function_that_may_take_time()
    end)
    
    expect(success).to_be_true()
    expect(result).to_equal("expected")
end)
```

### Testing error cases

```lua
it("should throw an error", function()
    local success, err = pcall(function()
        function_that_should_error()
    end)
    
    expect(success).to_be_false()
    expect(tostring(err)).to_match("expected error pattern")
end)
```

## Registering Tests in Your Application

To register tests in your application, you can use a YAML configuration file (typically `_index.yaml`):

```yaml
version: "1.0"
namespace: app.test.cases

meta:
  depends_on: [ "ns:system", "ns:wippy.test" ]

entries:
  - name: system_test
    kind: function.lua
    meta:
      name: "System Test"
      type: "test"
      tags: [ "system", "monitoring", "health", "tests" ]
      comment: "Performs a basic system health check"
    source: file://system_test.lua
    method: run_tests
    modules: [ "http_client", "fs", "time", "sql", "crypto" ]
    imports:
      test: "wippy.test:test"
```

Your test file (`system_test.lua`) should export the method specified in the YAML config:

```lua
-- Import dependencies
local test = require("test")

-- Define the run_tests function that will be called
function run_tests(options)
    -- Define your test cases
    local function define_tests()
        describe("System Health", function()
            -- Your test cases here
        end)
    end
    
    -- Run the tests with the test framework
    return test.run_cases(define_tests)(options)
end

-- Return the run_tests function
return {
    run_tests = run_tests
}
```

The application framework will load your test module using the information in the YAML file and execute the specified method.