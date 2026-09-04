-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local entry_id = "app.test.registry:registry_metadata_surface"

local function state_entry(id)
	local snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	assert.not_nil(snap, "snapshot available")

	local state, state_err = snap:state()
	assert.is_nil(state_err, "snapshot state no error")
	assert.not_nil(state, "snapshot state available")

	for _, entry in ipairs(state.entries) do
		if entry.id == id then
			return entry
		end
	end
	return nil
end

local function apply(change)
	local snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	local changes = snap:changes()
	change(changes)
	local version, apply_err = changes:apply()
	assert.is_nil(apply_err, "apply changes")
	assert.not_nil(version, "apply returns version")
end

local function main()
	local original_version, version_err = registry.current_version()
	assert.is_nil(version_err, "current version no error")
	assert.not_nil(original_version, "have original version")

	-- Registry ownership is not entry input. These fields deliberately resemble
	-- registry metadata and must neither be accepted nor appear on ordinary reads.
	apply(function(changes)
		changes:create({
			id = entry_id,
			kind = "function.lua",
			root = true,
			owner = "untrusted/module",
			registry = { owner = "untrusted/module", root = true },
			meta = { comment = "registry metadata surface regression" },
			data = {
				source = "return { main = function() return true end }",
				method = "main",
			},
		})
	end)

	local ordinary, get_err = registry.get(entry_id)
	assert.is_nil(get_err, "ordinary read no error")
	assert.not_nil(ordinary, "ordinary entry exists")
	local author_entry = ordinary as any
	assert.is_nil(author_entry.root, "ordinary reads do not expose root")
	assert.is_nil(author_entry.owner, "ordinary reads do not expose owner")
	assert.is_nil(author_entry.registry, "ordinary reads do not expose registry metadata")

	local first_state = state_entry(entry_id)
	assert.not_nil(first_state, "state contains created entry")
	assert.not_nil(first_state.registry, "state carries registry metadata")
	assert.eq(first_state.registry.owner, "", "entry input cannot assign ownership")
	assert.eq(first_state.registry.root, false, "entry input cannot select a deployment root")

	-- A normal read-modify-write must leave registry-owned metadata intact.
	ordinary.meta.comment = "rewritten"
	author_entry.root = true
	author_entry.owner = "another/untrusted-module"
	author_entry.registry = { owner = "another/untrusted-module", root = true }
	apply(function(changes)
		changes:update(ordinary)
	end)

	local after_state = state_entry(entry_id)
	assert.not_nil(after_state, "state contains rewritten entry")
	assert.eq(after_state.registry.owner, "", "rewrite cannot change ownership")
	assert.eq(after_state.registry.root, false, "rewrite cannot change deployment-root state")
	assert.eq(after_state.meta.comment, "rewritten", "author data remains writable")

	local restored, restore_err = registry.apply_version(original_version)
	assert.is_nil(restore_err, "restore original version")
	assert.ok(restored, "restore original succeeded")

	return true
end

return { main = main }
