local funcs = require("funcs")

local function main(input)
	local name = input and input.name or "World"

	-- Call the echo activity via funcs.call
	local result, err = funcs.call("app.test.temporal.activities:echo_activity", {
		message = "Hello from workflow",
		name = name
	})

	if err then
		error("activity call failed: " .. tostring(err))
	end

	return {
		activity_result = result,
		workflow_input = input
	}
end

return main
