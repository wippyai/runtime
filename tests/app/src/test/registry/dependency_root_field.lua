-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

-- root marks an ns.dependency selected as a deployment root and is the sole
-- authority for that status: meta is user space and carries no trust. A writer
-- that cannot carry the field silently demotes every root it touches, and a
-- reader that cannot see it demotes the entry again on the next rewrite.
local function entry_for(id, root)
	return {
		id = id,
		kind = "function.lua",
		root = root,
		meta = {
			comment = "dependency root transport regression",
			module = "wippy/example",
		},
		data = {
			source = "return { main = function() return true end }",
			method = "main",
		},
	}
end

local function main()
	local original_version, version_err = registry.current_version()
	assert.is_nil(version_err, "current version no error")
	assert.not_nil(original_version, "have original version")

	local root_id = "app.test.registry:dependency_root_marked"
	local plain_id = "app.test.registry:dependency_root_unmarked"

	local snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	local changes = snap:changes()
	changes:create(entry_for(root_id, true))
	changes:create(entry_for(plain_id, false))
	local applied_version, apply_err = changes:apply()
	assert.is_nil(apply_err, "apply changeset")
	assert.not_nil(applied_version, "applied version returned")

	local marked, marked_err = registry.get(root_id)
	assert.is_nil(marked_err, "read marked entry")
	assert.not_nil(marked, "marked entry exists")
	assert.eq(marked.root, true, "root survives the write and the read")

	local unmarked, unmarked_err = registry.get(plain_id)
	assert.is_nil(unmarked_err, "read unmarked entry")
	assert.not_nil(unmarked, "unmarked entry exists")
	assert.eq(unmarked.root, false, "an unmarked entry stays unmarked")

	-- A read-modify-write is the path keeper takes on every dependency update.
	local snap2, snap2_err = registry.snapshot()
	assert.is_nil(snap2_err, "second snapshot no error")
	local rewrite = snap2:changes()
	marked.meta.comment = "rewritten"
	rewrite:update(marked)
	local rewritten_version, rewrite_err = rewrite:apply()
	assert.is_nil(rewrite_err, "apply rewrite")
	assert.not_nil(rewritten_version, "rewritten version returned")

	local after, after_err = registry.get(root_id)
	assert.is_nil(after_err, "read rewritten entry")
	assert.eq(after.root, true, "round trip through Lua does not demote a root")

	local restored, restore_err = registry.apply_version(original_version)
	assert.is_nil(restore_err, "restore original version")
	assert.ok(restored, "restore original succeeded")

	return true
end

return { main = main }
