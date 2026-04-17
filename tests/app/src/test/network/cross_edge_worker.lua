-- SPDX-License-Identifier: MPL-2.0

-- Inner process of the func->process->func chain. Does NOT perform HTTP
-- directly; instead calls overlay_callee via funcs.new() with no overlay
-- selection. If the ambient overlay has propagated into this process' ctx
-- and then into the funcs.call, the nested function's http.get routes
-- through the proxy. This proves process->funcs inheritance on top of the
-- outer funcs->process step.

local function main()
	local funcs = require("funcs")

	local probe, err = funcs.new():call("app.test.network:overlay_callee")
	if err ~= nil then
		local msg = type(err) == "table" and (err.message or tostring(err)) or tostring(err)
		return { ok = false, err = "funcs.call: " .. msg }
	end
	return probe
end

return { main = main }
