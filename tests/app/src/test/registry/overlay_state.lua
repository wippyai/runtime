-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local errors = require("errors")
local registry = require("registry")

local function main()
	local snap, overlay_err = registry.overlay("app.test.registry:state_overlay")
	assert.is_nil(overlay_err, "open overlay")
	assert.not_nil(snap, "overlay snapshot returned")

	local state, state_err = snap:state()
	assert.is_nil(state, "overlay state is unavailable without provenance")
	assert.not_nil(state_err, "overlay state fails closed")
	assert.eq(state_err:kind(), errors.UNAVAILABLE, "overlay state reports unavailable provenance")

	-- Existing overlay operations remain available. Only the paired provenance
	-- projection is refused because overlay storage does not own that evidence.
	local entries, entries_err = snap:entries()
	assert.is_nil(entries_err, "overlay entries remain readable")
	assert.not_nil(entries, "overlay entries returned")

	return true
end

return { main = main }
