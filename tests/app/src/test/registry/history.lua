-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- get history
	local hist, err = registry.history()
	assert.is_nil(err, "history no error")
	assert.not_nil(hist, "history returned")

	-- history has versions
	local versions = hist:versions()
	assert.not_nil(versions, "history has versions")
	assert.eq(type(versions), "table", "versions is table")
	assert.ok(#versions > 0, "history has versions")

	-- current version
	local current, err = registry.current_version()
	assert.is_nil(err, "current_version no error")
	assert.not_nil(current, "current version returned")

	-- version has id
	local vid = current:id()
	assert.not_nil(vid, "version has id")
	assert.eq(type(vid), "number", "version id is number")

	-- version tostring
	local str = tostring(current)
	assert.ok(string.find(str, "Version", 1, true), "version has tostring")

	-- versions list
	local vers, err = registry.versions()
	assert.is_nil(err, "versions no error")
	assert.not_nil(vers, "versions returned")
	assert.ok(#vers > 0, "versions list has entries")

	-- get snapshot at version
	local snap = hist:snapshot_at(current)
	assert.not_nil(snap, "snapshot_at works")

	-- history tostring
	str = tostring(hist)
	assert.ok(string.find(str, "History", 1, true), "history has tostring")

	return true
end

return { main = main }
