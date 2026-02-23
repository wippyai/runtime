-- SPDX-License-Identifier: MPL-2.0

local ctx = require("ctx")

local function main(input)
	local results = {}

	local user_id, err = ctx.get("user_id")
	if not err then
		results.user_id = user_id
	end

	local tenant, err = ctx.get("tenant")
	if not err then
		results.tenant = tenant
	end

	local request_id, err = ctx.get("request_id")
	if not err then
		results.request_id = request_id
	end

	local all_ctx, err = ctx.all()
	if not err and all_ctx then
		results.all_context = all_ctx
	end

	results.from_activity = true
	results.input = input

	return results
end

return main
