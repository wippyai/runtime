-- SPDX-License-Identifier: MPL-2.0

-- Selecting a SOCKS5 overlay that points at a closed port must fail at the
-- proxy dial layer, not at the final target. This is the smoking gun that
-- proves the overlay is actually engaged: a clearnet fallback would reach
-- the local app and return 200.

local assert = require("assert2")

local function main()
	local http = require("http_client")

	local resp, err = http.get("http://localhost:8085/hello", {
		overlay_network = "app.test.network:broken_socks5",
		timeout = "2s",
	})

	assert.is_nil(resp, "broken proxy must not return a response")
	assert.not_nil(err, "broken proxy must error")

	return true
end

return { main = main }
