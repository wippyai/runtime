local assert = require("assert2")
local funcs = require("funcs")

local function call_and_assert(id, expected, ...)
	local result, err = funcs.call(id, ...)
	assert.is_nil(err, id .. " should not error")
	assert.eq(result, expected, id .. " returned unexpected value")
end

local function main()
	-- Raw/core wasm loaded from filesystem (requires WIT on entry)
	call_and_assert("app.test.wasm:answer_wasm_fs", 42)

	-- Component wasm loaded from filesystem across different pool types
	call_and_assert("app.test.wasm:compute_component_inline", 42, 6, 7)
	call_and_assert("app.test.wasm:compute_component_lazy", 42, 6, 7)
	call_and_assert("app.test.wasm:compute_component_static", 42, 6, 7)
	call_and_assert("app.test.wasm:compute_component_adaptive", 42, 6, 7)
	call_and_assert("app.test.wasm:compute_component_wasi", 42, 6, 7)

	return true
end

return { main = main }
