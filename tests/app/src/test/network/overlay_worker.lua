-- SPDX-License-Identifier: MPL-2.0

-- Worker that probes the ambient overlay set on the process by
-- process.with_options({network = ...}). Does a plain http.get without any
-- explicit overlay_network; if the overlay is wired through context the
-- dial goes via the proxy. Returns { ok, status, err } to the spawner.

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
