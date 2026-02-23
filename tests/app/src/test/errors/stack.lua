-- SPDX-License-Identifier: MPL-2.0

-- Test: error stack traces
local assert = require("assert2")

local function main()
	local function inner_func()
		return errors.new("stack test")
	end
	local function outer_func()
		return inner_func()
	end

	local err = outer_func()

	-- err:stack() returns string
	local stack = err:stack()
	assert.ok(stack, "stack exists")
	assert.ok(#stack > 0, "stack not empty")

	-- errors.call_stack returns structured data
	local cs = errors.call_stack(err)
	assert.ok(cs, "call_stack returns table")
	if cs then
		assert.ok(cs.frames, "has frames")
		if cs.frames and #cs.frames > 0 then
			local frame = cs.frames[1]
			if frame then
				assert.ok(frame.line, "frame has line")
				assert.ok(frame.source, "frame has source")
			end
		end
	end

	return true
end

return { main = main }
