-- SPDX-License-Identifier: MPL-2.0

-- Child whose entry configures an allow-all policy. Started by an eval it
-- still runs under the eval's policy; it reports what it may do.
local funcs = require("funcs")
local registry = require("registry")
local env = require("env")

local function main()
	local report = {}
	local called, call_err = funcs.call("app.test.eval_module:fanout")
	report.funcs = called == nil and tostring(call_err) or "allowed"
	local entry, entry_err = registry.get("app.test.eval_module:fanout")
	report.registry = entry == nil and tostring(entry_err) or "allowed"
	local value, env_err = env.get("HOME")
	report.env = value == nil and tostring(env_err) or "allowed"
	return report
end

return { main = main }
