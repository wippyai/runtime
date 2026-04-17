-- SPDX-License-Identifier: MPL-2.0

-- funcs.with_options({network = ...}) must apply the overlay for the entire
-- lifecycle of the called function, not just the outer caller. The callee
-- here makes a plain http.get with no explicit overlay_network; if the
-- ambient overlay is honored we hit the broken proxy and get a dial error.

local assert = require("assert2")

local function main()
	local funcs = require("funcs")

	local result, err = funcs.new()
	:with_options({ network = "app.test.network:broken_socks5" })
	:call("app.test.network:overlay_callee")

	assert.is_nil(err, "call itself did not fail: " .. tostring(err))
	assert.not_nil(result, "callee returned a result table")

	-- When the overlay is engaged we cannot reach the real target; the callee
	-- reports ok=false and the error describes the proxy-dial failure.
	assert.eq(result.ok, false, "callee must not reach clearnet target through overlay")
	assert.not_nil(result.err, "callee surfaced proxy error")

	return true
end

return { main = main }
