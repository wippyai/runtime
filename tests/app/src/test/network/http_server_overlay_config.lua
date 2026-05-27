-- SPDX-License-Identifier: MPL-2.0

-- Verifies the http.service YAML pipeline keeps the `network:` field and
-- stores it as data.network on the registry entry. The boot component
-- registers `data.network` as a DependencyPattern so the topology resolver
-- orders the HTTP service after its backing network. This test doesn't try
-- to bind the service (SOCKS5 does not support Listen); it checks that the
-- config round-trip preserves the network reference.

local assert = require("assert2")
local registry = require("registry")

local function main()
	local entry, err = registry.get("app.test.network:overlay_http_service")
	assert.is_nil(err, "overlay_http_service lookup: " .. tostring(err))
	assert.not_nil(entry, "overlay_http_service entry exists")
	assert.eq(entry.kind, "http.service", "kind is http.service")

	assert.not_nil(entry.data, "entry has data")
	assert.eq(type(entry.data), "table", "data is table")

	-- The YAML source uses `network: app.test.network:fast_socks5`; the
	-- transcoder must surface it as data.network on the entry.
	local network = entry.data.network
	assert.not_nil(network, "data.network is present")
	-- registry.ID is serialized as a string "ns:name".
	assert.eq(tostring(network), "app.test.network:fast_socks5",
		"data.network points at fast_socks5")

	-- Sanity: the addr field survives the round-trip alongside network.
	assert.eq(entry.data.addr, "127.0.0.1:0", "data.addr preserved")

	return true
end

return { main = main }
