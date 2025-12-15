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
