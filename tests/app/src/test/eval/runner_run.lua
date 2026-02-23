-- SPDX-License-Identifier: MPL-2.0

-- Test: eval_runner run function
local assert = require("assert")

local function main()
	local runner = require("eval_runner")

	-- Test run with simple return
	local result, err = runner.run({
		source = [[
            local function handle(x)
                return x * 2
            end
            return { handle = handle }
        ]],
		method = "handle",
		args = { 21 },
		modules = { "json" }
	})

	assert.no_error(result, err, "run should succeed")
	assert.eq(result, 42, "should return 42")

	-- Test run with no args
	local result2, err2 = runner.run({
		source = [[
            return { main = function() return "hello" end }
        ]],
		method = "main",
		modules = {}
	})

	assert.no_error(result2, err2, "run without args should succeed")
	assert.eq(result2, "hello", "should return 'hello'")

	-- Test run with table result
	local result3, err3 = runner.run({
		source = [[
            return {
                get_data = function()
                    return { name = "test", value = 123 }
                end
            }
        ]],
		method = "get_data",
		modules = { "json" }
	})

	assert.no_error(result3, err3, "run with table result should succeed")
	assert.eq(result3.name, "test", "table result should have name")
	assert.eq(result3.value, 123, "table result should have value")

	-- Test run with json module
	local result4, err4 = runner.run({
		source = [[
            local json = require("json")
            return {
                encode = function(data)
                    return json.encode(data)
                end
            }
        ]],
		method = "encode",
		args = { { foo = "bar" } },
		modules = { "json" }
	})

	assert.no_error(result4, err4, "run with json should succeed")
	assert.is_string(result4, "result should be string")
	assert.contains(result4, "foo", "result should contain foo")
	assert.contains(result4, "bar", "result should contain bar")

	-- Test run with custom_modules (inject custom table as global)
	local result5, err5 = runner.run({
		source = [[
            return {
                get_version = function()
                    return my_sdk.version
                end
            }
        ]],
		method = "get_version",
		modules = {},
		custom_modules = {
			my_sdk = { version = "1.2.3", name = "TestSDK" }
		}
	})

	assert.no_error(result5, err5, "run with custom_modules should succeed")
	assert.eq(result5, "1.2.3", "should return injected version")

	-- Test run with custom_modules accessing nested data
	local result6, err6 = runner.run({
		source = [[
            return {
                get_url = function()
                    return config.api.base_url
                end
            }
        ]],
		method = "get_url",
		modules = {},
		custom_modules = {
			config = {
				api = { base_url = "https://api.example.com" },
				debug = true
			}
		}
	})

	assert.no_error(result6, err6, "run with nested custom_modules should succeed")
	assert.eq(result6, "https://api.example.com", "should return nested value")

	return true
end

return { main = main }
