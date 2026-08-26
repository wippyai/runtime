-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

-- Root status is registry-owned provenance. Entry authors neither stamp it in
-- meta nor write it through the entry transport; set_root is the dedicated
-- mutation boundary and ordinary entry rewrites must preserve it.
local function main()
	local original_version, version_err = registry.current_version()
	assert.is_nil(version_err, "current version no error")
	assert.not_nil(original_version, "have original version")

	local root_id = "app.test.registry:dependency_root_transport"
	local snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	local changes = snap:changes()
	changes:create({
		id = root_id,
		kind = "ns.dependency",
		data = {
			component = "wippy/terminal",
			version = ">=0.0.0",
		},
	})
	local installed_version, install_err = changes:apply()
	assert.is_nil(install_err, "install dependency")
	assert.not_nil(installed_version, "install version returned")

	local dependency, dependency_err = registry.get(root_id)
	assert.is_nil(dependency_err, "read dependency")
	local original_root = dependency.root == true
	local changed_root = not original_root

	local changed, root_err = registry.set_root(root_id, changed_root)
	assert.is_nil(root_err, "change dependency root")
	assert.ok(changed, "root status changed")

	local marked, marked_err = registry.get(root_id)
	assert.is_nil(marked_err, "read marked entry")
	assert.not_nil(marked, "marked entry exists")
	assert.eq(marked.root, changed_root, "root survives the write and the read")

	-- A read-modify-write is the path keeper takes on every dependency update.
	local snap2, snap2_err = registry.snapshot()
	assert.is_nil(snap2_err, "second snapshot no error")
	local rewrite = snap2:changes()
	marked.root = nil
	marked.meta = marked.meta or {}
	marked.meta.comment = "rewritten"
	rewrite:update(marked)
	local rewritten_version, rewrite_err = rewrite:apply()
	assert.is_nil(rewrite_err, "apply rewrite")
	assert.not_nil(rewritten_version, "rewritten version returned")

	local after, after_err = registry.get(root_id)
	assert.is_nil(after_err, "read rewritten entry")
	assert.eq(after.root, changed_root, "round trip through Lua does not change root state")

	local restored, restore_err = registry.apply_version(original_version)
	assert.is_nil(restore_err, "restore original version")
	assert.ok(restored, "restore original succeeded")

	return true
end

return { main = main }
