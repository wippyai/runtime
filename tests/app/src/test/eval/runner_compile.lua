-- Test: eval_runner compile function
local assert = require("assert")

local function main()
	local runner = require("eval_runner")

	-- Test compile returns a program
	local program, err = runner.compile([[
        local function handle(x)
            return x * 2
        end
        return { handle = handle }
    ]], "handle", { modules = { "json" } })

	assert.no_error(program, err, "compile should succeed")
	assert.not_nil(program, "program should not be nil")

	-- Test program has expected methods
	assert.eq(program:method(), "handle", "program method should be 'handle'")

	local modules = program:modules()
	assert.not_nil(modules, "modules should not be nil")
	assert.eq(modules[1], "json", "first module should be json")

	-- Test compile with no method
	local program2, err2 = runner.compile([[
        return { main = function() return 42 end }
    ]], "", { modules = { "json" } })

	assert.no_error(program2, err2, "compile with empty method should succeed")
	assert.eq(program2:method(), "", "empty method should be preserved")

	-- Test compile with syntax error
	local program3, err3 = runner.compile([[
        this is not valid lua!!!
    ]], "handle", { modules = { "json" } })

	assert.is_nil(program3, "syntax error should return nil program")
	assert.not_nil(err3, "syntax error should return error")

	return true
end

return { main = main }
