-- SPDX-License-Identifier: MPL-2.0

-- Test: sql basic operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	-- Get database connection
	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database without error")
	assert.not_nil(db, "should have database connection")

	-- Check database type
	local dbtype, err2 = db:type()
	assert.is_nil(err2, "type should not error")
	assert.eq(dbtype, sql.type.SQLITE, "should be sqlite database")

	-- Get connection stats
	local stats, err3 = db:stats()
	assert.is_nil(err3, "stats should not error")
	assert.not_nil(stats, "should have stats")
	assert.not_nil(stats.open_connections, "should have open_connections")
	assert.not_nil(stats.in_use, "should have in_use")
	assert.not_nil(stats.idle, "should have idle")

	-- Release connection
	local ok, err4 = db:release()
	assert.is_nil(err4, "release should not error")
	assert.eq(ok, true, "release should return true")

	return true
end

return { main = main }
