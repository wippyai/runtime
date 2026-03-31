-- SPDX-License-Identifier: MPL-2.0

local store = require("store")

local function main()
	local s, err = store.get("app.test.store:memory")
	if err then
		return nil, err
	end

	local count = s:get("always_fail_count") or 0
	count = count + 1
	s:set("always_fail_count", count)

	return nil, errors.new({
		message = "always fails (attempt " .. count .. ")",
		kind = errors.UNAVAILABLE,
		retryable = true,
	})
end

return { main = main }
