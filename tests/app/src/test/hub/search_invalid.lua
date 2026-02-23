-- SPDX-License-Identifier: MPL-2.0

local function main()
	local assert = assert2

	local res, err = hub.modules.search("")
	assert.has_error(res, err, "empty query should error")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
end

return { main = main }
