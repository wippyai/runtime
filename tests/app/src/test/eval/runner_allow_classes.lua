-- Test: eval_runner allow_classes feature
local assert = require("assert")

local function main()
	local runner = require("eval_runner")

	-- Test that http_client is blocked without allow_classes (network class forbidden)
	local result1, err1 = runner.run({
		source = [[
            local http = require("http_client")
            return { run = function() return "should not reach" end }
        ]],
		method = "run",
		modules = { "http_client" }
	})

	-- Should fail - http_client has network class which is forbidden
	assert.is_nil(result1, "should fail without allow_classes")
	assert.not_nil(err1, "should have error without allow_classes")
	assert.contains(tostring(err1), "forbidden", "error should mention forbidden class")

	-- Test that http_client works with allow_classes = {"network"}
	local result2, err2 = runner.run({
		source = [[
            local http = require("http_client")
            return {
                check = function()
                    return type(http.get) == "function"
                end
            }
        ]],
		method = "check",
		modules = { "http_client" },
		allow_classes = { "network", "io" }
	})

	assert.no_error(result2, err2, "should succeed with allow_classes")
	assert.eq(result2, true, "http_client.get should be a function")

	return true
end

return { main = main }
