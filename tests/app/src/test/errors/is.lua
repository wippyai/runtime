-- SPDX-License-Identifier: MPL-2.0

-- Test: errors.is checks error kind
local assert = require("assert2")

local function main()
	local e = errors.new({message = "timeout", kind = errors.TIMEOUT})

	assert.eq(errors.is(e, errors.TIMEOUT), true, "matches correct kind")
	assert.eq(errors.is(e, errors.NOT_FOUND), false, "rejects wrong kind")
	assert.eq(errors.is("not an error", errors.TIMEOUT), false, "rejects non-error")
	assert.eq(errors.is(nil, errors.TIMEOUT), false, "rejects nil")

	return true
end

return { main = main }
