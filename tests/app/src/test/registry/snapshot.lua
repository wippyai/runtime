-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- get snapshot
	local snap, err = registry.snapshot()
	assert.is_nil(err, "snapshot no error")
	assert.not_nil(snap, "snapshot returned")

	-- snapshot has version
	local version = snap:version()
	assert.not_nil(version, "snapshot has version")

	-- snapshot has entries method
	local entries = snap:entries()
	assert.not_nil(entries, "snapshot has entries")
	assert.eq(type(entries), "table", "entries is table")

	-- State pairs the author entries with the registry-owned provenance captured
	-- at the same version. The provenance map is total over visible entries and
	-- is returned by value, so consumers can index it without N live lookups.
	local state, state_err = snap:state()
	assert.is_nil(state_err, "snapshot state no error")
	assert.not_nil(state, "snapshot state returned")
	assert.eq(#state.entries, #entries, "state and entries expose the same visible set")
	local provenance_count = 0
	for _, entry in ipairs(state.entries) do
		assert.not_nil(state.provenance[entry.id], "every visible entry has provenance: " .. entry.id)
	end
	for _ in pairs(state.provenance) do provenance_count = provenance_count + 1 end
	assert.eq(provenance_count, #state.entries, "snapshot state exposes no orphaned provenance")

	if #state.entries > 0 then
		local first_id = state.entries[1].id
		state.provenance[first_id].module = "forged/module"
		state.entries[1].meta.module = "forged/module"
		local fresh, fresh_err = snap:state()
		assert.is_nil(fresh_err, "repeat snapshot state no error")
		assert.ok(fresh.provenance[first_id].module ~= "forged/module", "provenance result is detached")
		assert.ok(fresh.entries[1].meta.module ~= "forged/module", "entry result is detached")
	end

	-- namespace filter
	local ns_entries = snap:namespace("app")
	assert.not_nil(ns_entries, "namespace filter works")
	assert.eq(type(ns_entries), "table", "namespace returns table")

	-- find from snapshot
	local found = snap:find({type = "test"})
	assert.not_nil(found, "find from snapshot works")
	assert.eq(type(found), "table", "find returns table")

	-- snapshot tostring
	local str = tostring(snap)
	assert.ok(string.find(str, "Snapshot", 1, true), "snapshot has tostring")

	return true
end

return { main = main }
