-- SPDX-License-Identifier: MPL-2.0

local store = require("store")

local function main()
	local s, err = store.get("app.test.store:memory")
	if err then
		return nil, err
	end

	local count = s:get("permanent_fail_count") or 0
	count = count + 1
	s:set("permanent_fail_count", count)

	return nil, errors.new({
		message = "permission denied",
		kind = errors.PERMISSION_DENIED,
		retryable = false,
	})
end

return { main = main }
