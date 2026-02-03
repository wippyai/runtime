local assert = require("assert2")
local registry = require("registry")

local function main()
-- get all versions
	local versions, err = registry.versions()
	assert.is_nil(err, "versions no error")
	assert.not_nil(versions, "versions returned")
	assert.ok(#versions > 0, "has versions")

	-- get history for snapshot_at
	local hist, hist_err = registry.history()
	assert.is_nil(hist_err, "history no error")

	-- compare snapshots at different versions
	if #versions >= 2 then
		local v1 = versions[1]
		local v2 = versions[#versions] -- latest
		assert.not_nil(v1, "should have first version")
		assert.not_nil(v2, "should have latest version")

		local snap1 = hist:snapshot_at(v1)
		local snap2 = hist:snapshot_at(v2)

		assert.not_nil(snap1, "snapshot at v1 exists")
		assert.not_nil(snap2, "snapshot at v2 exists")

		local entries1 = snap1:entries()
		local entries2 = snap2:entries()

		-- later versions may have more entries
		assert.ok(#entries2 >= #entries1, "later version has >= entries")

		-- verify version info is different
		local sv1 = snap1:version()
		local sv2 = snap2:version()
		assert.not_nil(sv1, "snap1 has version")
		assert.not_nil(sv2, "snap2 has version")
		assert.ok(sv1:id() ~= sv2:id(), "versions are different")
	end

	-- test snapshot changes detection
	local current_snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	assert.not_nil(current_snap, "current snapshot exists")

	-- same version snapshot should show no changes
	local changes = current_snap:changes()
	assert.not_nil(changes, "changes returned")

	-- ops returns the list of operations in the changeset
	local ops = changes:ops()
	assert.not_nil(ops, "ops returned")
	assert.eq(type(ops), "table", "ops is table")
	assert.eq(#ops, 0, "no changes between same snapshots")

	-- changes tostring
	local changes_str = tostring(changes)
	assert.ok(string.find(changes_str, "Changes", 1, true), "changes has tostring")

	-- test changes builder methods (they add operations, not read them)
	local test_entry = {
		id = "test.ns:test_entry",
		kind = "test.kind",
		meta = { test = true },
		data = { value = 123 }
	}

	-- create a fresh changeset
	local build_snap, _ = registry.snapshot()
	local builder = build_snap:changes()

	-- create() adds a create operation
	local chained = builder:create(test_entry)
	assert.not_nil(chained, "create returns changeset")

	-- update() adds an update operation
	chained = builder:update(test_entry)
	assert.not_nil(chained, "update returns changeset")

	-- delete() adds a delete operation (takes id)
	chained = builder:delete("test.ns:test_entry")
	assert.not_nil(chained, "delete returns changeset")

	-- verify ops count
	local build_ops = builder:ops()
	assert.eq(#build_ops, 3, "builder has 3 ops")

	-- verify operation kinds (use actual kind values)
	assert.eq(build_ops[1].kind, "entry.create", "first op is entry.create")
	assert.eq(build_ops[2].kind, "entry.update", "second op is entry.update")
	assert.eq(build_ops[3].kind, "entry.delete", "third op is entry.delete")

	return true
end

return { main = main }
