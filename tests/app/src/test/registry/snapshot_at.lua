-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- get current version
	local current, err = registry.current_version()
	assert.is_nil(err, "current_version no error")
	assert.not_nil(current, "current version returned")

	-- get version id
	local version_id = current:id()
	assert.eq(type(version_id), "number", "version id is number")

	-- get all versions to find one we can test with
	local versions, vers_err = registry.versions()
	assert.is_nil(vers_err, "versions no error")
	assert.not_nil(versions, "versions returned")
	assert.ok(#versions > 0, "has at least one version")

	-- find a version with id > 0 for snapshot_at test
	local test_version = nil
	for _, v in ipairs(versions) do
		if v:id() > 0 then
			test_version = v
			break
		end
	end

	-- if we have a version > 0, test snapshot_at
	if test_version then
		local test_id = test_version:id()

		local snap, snap_err = registry.snapshot_at(test_id)
		assert.is_nil(snap_err, "snapshot_at no error")
		assert.not_nil(snap, "snapshot_at returned snapshot")

		local snap_version = snap:version()
		assert.not_nil(snap_version, "snapshot has version")
		assert.eq(snap_version:id(), test_id, "snapshot version matches requested")

		local entries = snap:entries()
		assert.not_nil(entries, "snapshot has entries")
	end

	-- test snapshot_at with invalid version (0)
	local bad_snap, bad_err = registry.snapshot_at(0)
	assert.is_nil(bad_snap, "invalid version 0 returns nil")
	assert.not_nil(bad_err, "invalid version 0 returns error")

	-- test snapshot_at with negative version
	local neg_snap, neg_err = registry.snapshot_at(-1)
	assert.is_nil(neg_snap, "negative version returns nil")
	assert.not_nil(neg_err, "negative version returns error")

	-- test snapshot_at with non-existent version
	local missing_snap, missing_err = registry.snapshot_at(999999)
	assert.is_nil(missing_snap, "non-existent version returns nil")
	assert.not_nil(missing_err, "non-existent version returns error")

	-- test history.snapshot_at method (uses version object, not id)
	local hist, hist_err = registry.history()
	assert.is_nil(hist_err, "history no error")
	assert.not_nil(hist, "history returned")

	-- history:snapshot_at takes a version object
	local hist_snap = hist:snapshot_at(current)
	assert.not_nil(hist_snap, "history:snapshot_at works with current version")

	local hist_snap_version = hist_snap:version()
	assert.not_nil(hist_snap_version, "history snapshot has version")
	assert.eq(hist_snap_version:id(), version_id, "history snapshot version matches")

	return true
end

return { main = main }
