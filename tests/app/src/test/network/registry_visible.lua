-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
	local fast, err1 = registry.get("app.test.network:fast_socks5")
	assert.is_nil(err1, "fast_socks5 lookup: " .. tostring(err1))
	assert.not_nil(fast, "fast_socks5 entry exists")
	assert.eq(fast.kind, "network.socks5", "fast_socks5 kind")

	local broken, err2 = registry.get("app.test.network:broken_socks5")
	assert.is_nil(err2, "broken_socks5 lookup: " .. tostring(err2))
	assert.not_nil(broken, "broken_socks5 entry exists")
	assert.eq(broken.kind, "network.socks5", "broken_socks5 kind")

	return true
end

return { main = main }
