-- SPDX-License-Identifier: MPL-2.0

local sql = require("sql")

local function connect()
	local db, err = sql.get("app.test.sql:testdb")
	if err then
		error(err:message())
	end
	return db
end

local function get_connection()
	return connect()
end

local function main(): boolean
	local db = get_connection()
	local rows, err = db:query("SELECT 1")
	db:release()
	return rows ~= nil or err == nil
end

return { main = main }
