-- SPDX-License-Identifier: MPL-2.0

local workflow = require("workflow")

local function main(input)
	local crypto_result, err = workflow.exec("app.test.temporal.workflows:crypto_workflow", {
		message = input and input.message or "side effects",
	})
	if err then
		return { status = "crypto_error", error = tostring(err) }
	end

	local uuid_result, err = workflow.exec("app.test.temporal.workflows:uuid_workflow", {
		count = input and input.count or 2,
	})
	if err then
		return { status = "uuid_error", error = tostring(err) }
	end

	return {
		status = "completed",
		crypto = crypto_result,
		uuids = uuid_result,
	}
end

return main
