-- SPDX-License-Identifier: MPL-2.0

-- An overlay_network id that does not resolve to a registered network must
-- produce an error rather than silently falling back to clearnet. Leaking
-- DNS/target IP to the local network would defeat the whole point of overlays.

local assert = require("assert2")

local function main()
	local http = require("http_client")

	local resp, err = http.get("http://localhost:8085/hello", {
		overlay_network = "app.test.network:does_not_exist",
		timeout = "1s",
	})

	assert.is_nil(resp, "no response when overlay network unknown")
	assert.not_nil(err, "unknown overlay network must error")

	local msg = type(err) == "table" and (err.message or tostring(err)) or tostring(err)
	-- Either the registry rejects the id or the handler refuses to proceed;
	-- both are acceptable as long as no clearnet request was made.
	local ok = string.find(msg, "overlay", 1, true)
		or string.find(msg, "network", 1, true)
		or string.find(msg, "not found", 1, true)
	assert.ok(ok, "error mentions overlay/network: " .. msg)

	return true
end

return { main = main }
