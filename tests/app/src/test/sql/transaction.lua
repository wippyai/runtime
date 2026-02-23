-- SPDX-License-Identifier: MPL-2.0

-- Test: sql transaction operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	db:execute("CREATE TABLE IF NOT EXISTS tx_test (id INTEGER PRIMARY KEY, name TEXT)")
	db:execute("DELETE FROM tx_test")

	-- Test commit
	local tx1, err1 = db:begin()
	assert.is_nil(err1, "begin should not error")
	assert.not_nil(tx1, "should have transaction")

	-- Check transaction type
	local txtype, err_type = tx1:db_type()
	assert.is_nil(err_type, "db_type should not error")
	assert.eq(txtype, sql.type.SQLITE, "should be sqlite")

	-- Insert in transaction
	local _, err2 = tx1:execute("INSERT INTO tx_test (name) VALUES (?)", {"committed"})
	assert.is_nil(err2, "tx execute should not error")

	-- Commit
	local ok1, err3 = tx1:commit()
	assert.is_nil(err3, "commit should not error")
	assert.eq(ok1, true, "commit should return true")

	-- Verify data persisted
	local rows1, _ = db:query("SELECT name FROM tx_test")
	assert.eq(#rows1, 1, "should have 1 row after commit")
	assert.eq(rows1[1].name, "committed", "should have committed data")

	-- Test rollback
	local tx2, err4 = db:begin()
	assert.is_nil(err4, "second begin should not error")

	tx2:execute("INSERT INTO tx_test (name) VALUES (?)", {"rolled_back"})

	-- Verify data visible in transaction
	local rows2, _ = tx2:query("SELECT COUNT(*) as cnt FROM tx_test")
	local row2 = rows2[1]
	assert.not_nil(row2, "should have row")
	assert.eq(row2.cnt, 2, "should see 2 rows in transaction")

	-- Rollback
	local ok2, err5 = tx2:rollback()
	assert.is_nil(err5, "rollback should not error")
	assert.eq(ok2, true, "rollback should return true")

	-- Verify data not persisted
	local rows3, _ = db:query("SELECT COUNT(*) as cnt FROM tx_test")
	local row3 = rows3[1]
	assert.not_nil(row3, "should have row")
	assert.eq(row3.cnt, 1, "should still have 1 row after rollback")

	-- Test transaction with isolation level
	local tx3, err6 = db:begin({isolation = sql.isolation.SERIALIZABLE})
	assert.is_nil(err6, "begin with isolation should not error")
	tx3:rollback()

	-- Test read-only transaction
	local tx4, err7 = db:begin({read_only = true})
	assert.is_nil(err7, "begin read_only should not error")

	local _, err8 = tx4:query("SELECT * FROM tx_test")
	assert.is_nil(err8, "read in read_only tx should work")
	tx4:rollback()

	-- Test savepoint
	local tx5, _ = db:begin()
	tx5:execute("INSERT INTO tx_test (name) VALUES (?)", {"before_savepoint"})

	-- Create savepoint
	local ok_sp, err_sp = tx5:savepoint("sp1")
	assert.is_nil(err_sp, "savepoint should not error")
	assert.eq(ok_sp, true, "savepoint should return true")

	-- Insert after savepoint
	tx5:execute("INSERT INTO tx_test (name) VALUES (?)", {"after_savepoint"})

	-- Verify both rows visible
	local rows5, _ = tx5:query("SELECT COUNT(*) as cnt FROM tx_test WHERE name LIKE '%savepoint'")
	local row5 = rows5[1]
	assert.not_nil(row5, "should have row")
	assert.eq(row5.cnt, 2, "should see 2 savepoint rows")

	-- Rollback to savepoint
	local ok_rb, err_rb = tx5:rollback_to("sp1")
	assert.is_nil(err_rb, "rollback_to should not error")
	assert.eq(ok_rb, true, "rollback_to should return true")

	-- Verify only before_savepoint remains
	local rows6, _ = tx5:query("SELECT COUNT(*) as cnt FROM tx_test WHERE name LIKE '%savepoint'")
	local row6 = rows6[1]
	assert.not_nil(row6, "should have row")
	assert.eq(row6.cnt, 1, "should see 1 row after rollback_to")

	-- Release savepoint (optional cleanup)
	local ok_rel, err_rel = tx5:release("sp1")
	assert.is_nil(err_rel, "release savepoint should not error")
	assert.eq(ok_rel, true, "release should return true")

	tx5:commit()

	-- Test nested savepoints
	local tx6, _ = db:begin()
	tx6:execute("INSERT INTO tx_test (name) VALUES (?)", {"nested_base"})

	tx6:savepoint("outer")
	tx6:execute("INSERT INTO tx_test (name) VALUES (?)", {"nested_outer"})

	tx6:savepoint("inner")
	tx6:execute("INSERT INTO tx_test (name) VALUES (?)", {"nested_inner"})

	-- Rollback inner savepoint
	tx6:rollback_to("inner")

	-- Verify inner was rolled back but outer remains
	local rows7, _ = tx6:query("SELECT name FROM tx_test WHERE name LIKE 'nested%' ORDER BY name")
	assert.eq(#rows7, 2, "should have 2 nested rows")
	assert.eq(rows7[1].name, "nested_base", "first should be base")
	assert.eq(rows7[2].name, "nested_outer", "second should be outer")

	tx6:commit()

	-- Test invalid savepoint name
	local tx7, _ = db:begin()
	local _, err_invalid = tx7:savepoint("invalid name with spaces")
	assert.not_nil(err_invalid, "invalid savepoint name should error")
	tx7:rollback()

	-- Cleanup
	db:execute("DROP TABLE tx_test")
	db:release()

	return true
end

return { main = main }
