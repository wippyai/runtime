-- SPDX-License-Identifier: MPL-2.0

local store = require("store")

local function main()
	local s, err = store.get("app.test.store:memory")
	if err then
		return nil, err
	end

	local count = s:get("flaky_call_count") or 0
	count = count + 1
	s:set("flaky_call_count", count)

	if count < 3 then
		return nil, errors.new({
			message = "temporary failure (attempt " .. count .. ")",
			kind = errors.UNAVAILABLE,
			retryable = true,
		})
	end

	return "success_after_" .. count .. "_attempts"
end

return { main = main }
