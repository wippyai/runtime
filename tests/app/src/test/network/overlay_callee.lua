-- SPDX-License-Identifier: MPL-2.0

-- Probes the ambient overlay by doing http.get with no explicit overlay_network.
-- The overlay is expected to be inherited from the caller's funcs.with_options
-- selection. Target is the local app gateway; when an overlay is active, the
-- proxy receives the request and we observe the outcome (success or dial error
-- against the proxy itself). When no overlay is active, the app gateway answers.

local function main(args)
	local http = require("http_client")
	local url = (args and args.url) or "http://localhost:8085/hello"

	local resp, err = http.get(url, { timeout = "2s" })
	if err ~= nil then
		local msg = type(err) == "table" and (err.message or tostring(err)) or tostring(err)
		return { ok = false, err = msg }
	end
	return { ok = true, status = resp.status_code }
end

return { main = main }
