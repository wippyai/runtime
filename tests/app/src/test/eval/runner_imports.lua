-- SPDX-License-Identifier: MPL-2.0

-- Test: eval_runner imports feature
local assert = require("assert")

local function main()
	local runner = require("eval_runner")

	-- Test run with imports - access imported library directly
	local result1, err1 = runner.run({
		source = [[
            return {
                get_version = function()
                    return sdk.version
                end
            }
        ]],
		method = "get_version",
		modules = {},
		imports = {
			sdk = "app.lib:test_sdk"
		}
	})

	assert.no_error(result1, err1, "run with imports should succeed")
	assert.eq(result1, "2.0.0", "should return imported SDK version")

	-- Test run with imports - use require() to access imported library
	local result2, err2 = runner.run({
		source = [[
            local my_sdk = require("sdk")
            return {
                greet = function(name)
                    return my_sdk.greet(name)
                end
            }
        ]],
		method = "greet",
		args = { "Test" },
		modules = {},
		imports = {
			sdk = "app.lib:test_sdk"
		}
	})

	assert.no_error(result2, err2, "run with imports via require should succeed")
	assert.eq(result2, "Hello, Test!", "should return greeting from imported SDK")

	-- Test run with imports - call library function
	local result3, err3 = runner.run({
		source = [[
            local sdk = require("sdk")
            return {
                add = function(a, b)
                    return sdk.add(a, b)
                end
            }
        ]],
		method = "add",
		args = { 10, 32 },
		modules = {},
		imports = {
			sdk = "app.lib:test_sdk"
		}
	})

	assert.no_error(result3, err3, "run with imports calling function should succeed")
	assert.eq(result3, 42, "should return sum from imported SDK")

	-- Test run with imports - create object with methods
	local result4, err4 = runner.run({
		source = [[
            local sdk = require("sdk")
            return {
                create_and_get = function(name, value)
                    local obj = sdk.create_object(name, value)
                    return obj:get_info()
                end
            }
        ]],
		method = "create_and_get",
		args = { "test", 123 },
		modules = {},
		imports = {
			sdk = "app.lib:test_sdk"
		}
	})

	assert.no_error(result4, err4, "run with imports object methods should succeed")
	assert.eq(result4, "test: 123", "should return object info from imported SDK")

	-- Test run with multiple imports
	local result5, err5 = runner.run({
		source = [[
            local sdk = require("sdk")
            local json = require("json")
            return {
                encode_greeting = function(name)
                    return json.encode({ greeting = sdk.greet(name) })
                end
            }
        ]],
		method = "encode_greeting",
		args = { "World" },
		modules = { "json" },
		imports = {
			sdk = "app.lib:test_sdk"
		}
	})

	assert.no_error(result5, err5, "run with imports and modules should succeed")
	assert.contains(result5, "Hello, World!", "should contain greeting in JSON")

	return true
end

return { main = main }
