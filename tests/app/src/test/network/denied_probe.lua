-- SPDX-License-Identifier: MPL-2.0

-- Runs under the caller's security context. Tries to engage an overlay
-- network at each of the three Lua edges (httpclient, funcs, process) and
-- reports what each attempt produced. With a deny-network.select scope
-- injected, every attempt must fail with "not allowed".

local function to_msg(err)
	if err == nil then
		return nil
	end
	if type(err) == "table" then
		return err.message or tostring(err)
	end
	return tostring(err)
end

local function main(args)
	local target = (args and args.target) or "app.test.network:fast_socks5"
	local result = {}

	-- Edge 1: httpclient — denial returns (nil, err).
	local http = require("http_client")
	local resp, http_err = http.get("http://localhost:8085/hello", {
		overlay_network = target,
		timeout = "500ms",
	})
	result.http_resp = resp
	result.http_err = to_msg(http_err)

	-- Edge 2: funcs.with_options — denial returns (nil, err).
	local funcs = require("funcs")
	local exec, funcs_err = funcs.new():with_options({ network = target })
	result.funcs_exec = exec
	result.funcs_err = to_msg(funcs_err)

	-- Edge 3: process.with_options — denial raises via l.RaiseError.
	local ok, perr = pcall(function()
		return process.with_options({ network = target })
	end)
	result.process_ok = ok
	result.process_err = to_msg(perr)

	return result
end

return { main = main }
