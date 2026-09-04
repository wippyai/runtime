-- SPDX-License-Identifier: MPL-2.0

local io = require("io")
local sql = require("sql")
local cdc = require("cdc")
local channel = require("channel")
local time = require("time")

local function execute(db: sql.DB | sql.Transaction, statement: string)
    local _, err = db:execute(statement)
    if err then error(tostring(err)) end
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
