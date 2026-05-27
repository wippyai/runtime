-- SPDX-License-Identifier: MPL-2.0

-- The overlay must propagate across a func -> process -> func chain. The
-- test makes the outer selection via funcs.with_options({network = ...});
-- the first callee spawns a process with no options; the spawned process
-- issues a nested funcs.call whose target function ultimately runs the
-- http.get. For the bottom function's request to route through the proxy
-- the overlay has to survive BOTH boundary crossings (funcs->process and
-- process->funcs) purely via context inheritance.

local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local result, err = funcs.new()
	:with_options({ network = "app.test.network:broken_socks5" })
	:call("app.test.network:cross_edge_callee")

	assert.is_nil(err, "outer call failed: " .. tostring(err))
	assert.not_nil(result, "callee returned payload")

	assert.eq(result.ok, false, "spawned process must not reach clearnet through inherited overlay")
	assert.not_nil(result.err, "process surfaced proxy error")

	return true
end

return { main = main }
