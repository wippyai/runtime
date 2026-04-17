-- SPDX-License-Identifier: MPL-2.0

-- Isolation property: once a parent has selected an overlay, a descendant
-- cannot slip back to clearnet by running under a scope that denies
-- network.select. The permission gate deliberately fires only on *explicit*
-- selection; inherited ambient overlays bypass it so the parent's isolation
-- decision holds even when the child lacks permission to pick overlays.
--
-- Setup:
--   * funcs.with_options({network = broken_socks5}) sets the ambient overlay
--     in the callee's context.
--   * with_scope(deny-network.select) means the callee cannot itself perform
--     any explicit overlay selection.
--   * The callee does a plain http.get with no overlay_network argument.
--
-- Expected: the call routes through broken_socks5 (connection refused against
-- port 19999) rather than hitting the local app over clearnet. The surfaced
-- error must be the proxy dial failure, never "not allowed".

local assert = require("assert2")
local security = require("security")
local funcs = require("funcs")

local function main()
	local allow, err1 = security.policy("app.test.network:allow_everything")
	assert.is_nil(err1, "fetch allow_everything: " .. tostring(err1))
	local deny, err2 = security.policy("app.test.network:deny_network_select")
	assert.is_nil(err2, "fetch deny_network_select: " .. tostring(err2))

	local scope = security.new_scope():with(allow):with(deny)
	local actor = security.new_actor("inherited_bypass_user")

	local result, call_err = funcs.new()
	:with_actor(actor)
	:with_scope(scope)
	:with_options({ network = "app.test.network:broken_socks5" })
	:call("app.test.network:overlay_callee")

	assert.is_nil(call_err, "outer call failed: " .. tostring(call_err))
	assert.not_nil(result, "callee returned payload")

	assert.eq(result.ok, false, "ambient overlay must stay engaged under deny scope")
	assert.not_nil(result.err, "callee surfaced an error")

	local msg = tostring(result.err)
	local leaked = string.find(msg, "not allowed", 1, true)
	assert.is_nil(leaked, "inherited overlay must bypass the permission gate, got: " .. msg)

	return true
end

return { main = main }
