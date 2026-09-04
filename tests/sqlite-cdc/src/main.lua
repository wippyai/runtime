-- SPDX-License-Identifier: MPL-2.0

local io = require("io")
local sql = require("sql")
local cdc = require("cdc")
local channel = require("channel")
local time = require("time")
local registry = require("registry")

local function apply(change)
    local snapshot, err = registry.snapshot()
    assert(snapshot and not err, tostring(err))
    local changes = snapshot:changes()
    change(changes)
    local version, apply_err = changes:apply()
    assert(version and not apply_err, tostring(apply_err))
end

local function execute(db: sql.DB | sql.Transaction, statement: string)
    local _, err = db:execute(statement)
    if err then error(tostring(err)) end
end

-- Registry acceptance and supervisor readiness are separate contracts.
-- Bound the fixture's readiness wait; never sleep and assume activation.
local function await_source(id: string)
    local deadline = time.after("5s")
    while true do
        local info, err = cdc.source(id)
        assert(not err, tostring(err))
        if info and info.state == "running" then return end
        assert(not info or info.state ~= "faulted", "CDC source faulted")
        local tick = time.after("10ms")
        local result = channel.select{deadline:case_receive(), tick:case_receive()}
        assert(result.channel ~= deadline, "CDC source readiness timed out")
    end
end

local function receive(stream)
    local ch = stream:channel()
    local timeout = time.after("5s")
    local result = channel.select{ch:case_receive(), timeout:case_receive()}
    if result.channel ~= ch then error("CDC receive timed out") end
    if not result.ok then error("CDC stream closed") end
    return result.value
end

local function main()
    local db, db_err = sql.get("app:cdcdb")
    if db_err then error(tostring(db_err)) end
    execute(db, "CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT, data BLOB)")
    local stream, err = cdc.stream("app:changes", {tables={"items"}, buffer=16})
    if err then error(tostring(err)) end
    -- channel() performs subscription. Establish it before producing writes.
    stream:channel()
    execute(db, "INSERT INTO items VALUES(0, 'hello', x'616263')")
    local inserted = receive(stream)
    assert(inserted.op == "insert" and inserted.after.id == 0)
    assert(inserted.after.value == "hello")
    io.print("PASS CDC typed live insert, rowid zero")

    -- Create, replace and remove a second producer through the normal runtime
    -- registry, while the original producer and its subscription remain live.
    local entry = {
        id = "app:dynamic_changes", kind = "db.cdc.sqlite",
        data = {db_resource = "app:cdcdb", tables = {"items"},
                lifecycle = {auto_start = true}},
    }
    apply(function(changes) changes:create(entry) end)
    await_source(entry.id)
    local peer, peer_err = cdc.stream(entry.id, {tables={"items"}})
    assert(peer and not peer_err, tostring(peer_err))
    local peer_ch, subscribe_err = peer:channel()
    assert(peer_ch and not subscribe_err, tostring(subscribe_err))
    execute(db, "INSERT INTO items VALUES(3, 'fanout', NULL)")
    assert(receive(stream).after.id == 3)
    local peer_change = receive(peer)
    assert(peer_change.after.id == 3 and peer_change.source_id == entry.id)
    peer:close()
    apply(function(changes) changes:update(entry) end)
    local replacement, replacement_err = cdc.stream(entry.id, {tables={"items"}})
    assert(replacement and not replacement_err, tostring(replacement_err))
    local replacement_ch, replacement_sub_err = replacement:channel()
    assert(replacement_ch and not replacement_sub_err, tostring(replacement_sub_err))
    execute(db, "UPDATE items SET value='replaced' WHERE id=3")
    assert(receive(stream).after.value == "replaced")
    assert(receive(replacement).after.value == "replaced")
    replacement:close()
    apply(function(changes) changes:delete(entry.id) end)
    local removed, removed_err = cdc.source(entry.id)
    assert(not removed and not removed_err)
    execute(db, "DELETE FROM items WHERE id=3")
    assert(receive(stream).op == "delete")
    local denied, denied_err = cdc.stream("app:ungranted")
    assert(not denied and denied_err, "ungranted source must be denied")
    io.print("PASS CDC runtime create/update/delete, multi-source fanout and permissions")

    local tx, tx_err = db:begin()
    if tx_err then error(tostring(tx_err)) end
    execute(tx, "INSERT INTO items VALUES(1, 'rolled back', NULL)")
    local rolled, rollback_err = tx:rollback()
    assert(rolled and not rollback_err)
    execute(db, "INSERT INTO items VALUES(2, 'barrier', NULL)")
    local barrier = receive(stream)
    assert(barrier.after.id == 2, "rollback leaked into CDC")
    io.print("PASS CDC rollback isolation")

    local closed, close_err = stream:close()
    assert(closed and not close_err)
    local snapshot, snapshot_err = cdc.stream("app:changes", {tables={"items"}, snapshot=true, buffer=16})
    if snapshot_err then error(tostring(snapshot_err)) end
    local first, second = receive(snapshot), receive(snapshot)
    assert(first.op == "snapshot" and second.op == "snapshot")
    assert(first.after.id == 0 and second.after.id == 2)
    execute(db, "UPDATE items SET value='updated' WHERE id=0")
    local updated = receive(snapshot)
    assert(updated.op == "update" and updated.before.value == "hello" and updated.after.value == "updated")
    local released, release_err = snapshot:close()
    assert(released and not release_err)
    db:release()
    io.print("PASS CDC snapshot/live handoff, before images and close")
end

return {main = main}
