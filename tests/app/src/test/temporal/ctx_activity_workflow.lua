local ctx = require("ctx")
local funcs = require("funcs")

local function main(input)
	local results = {}

	local wf_user_id, err = ctx.get("user_id")
	if not err then
		results.workflow_user_id = wf_user_id
	end

	local result, err = funcs.call("app.test.temporal:ctx_activity", { from_workflow = true })
	if err then
		return { error = tostring(err) }
	end

	results.activity_result = result
	results.status = "completed"

	return results
end

return main
