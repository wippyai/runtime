-- Test: expr.compile and program:run
local assert = require("assert2")
local expr = require("expr")

local function main()
-- compile and run simple expression
	local program, err = expr.compile("a * b")
	assert.is_nil(err, "compile no error")
	assert.not_nil(program, "compile returns program")

	local result, err = program:run({a = 5, b = 6})
	assert.is_nil(err, "run no error")
	assert.eq(result, 30, "run result 5*6=30")

	-- run same program with different values
	result, err = program:run({a = 10, b = 3})
	assert.is_nil(err, "run second no error")
	assert.eq(result, 30, "run result 10*3=30")

	-- compile expression without variables
	program, err = expr.compile("2 + 2")
	assert.is_nil(err, "compile constant no error")

	result, err = program:run()
	assert.is_nil(err, "run constant no error")
	assert.eq(result, 4, "run constant result")

	-- compile with environment hint
	program, err = expr.compile("value * 2", {value = 0})
	assert.is_nil(err, "compile with env no error")

	result, err = program:run({value = 50})
	assert.is_nil(err, "run with env no error")
	assert.eq(result, 100, "run with env result")

	-- program has run method
	program, err = expr.compile("1")
	assert.is_nil(err, "compile 1 no error")
	assert.eq(type(program.run), "function", "program has run method")

	return true
end

return { main = main }
