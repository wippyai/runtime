-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local result, err = funcs.call("app.test.wasm:answer_wat")
	assert.is_nil(err, "wasm call should not error")
	assert.eq(result, 42, "wasm answer should be 42")
	return true
end

return { main = main }
