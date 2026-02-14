local sql = require("sql")

local function connect()
	local db, err = sql:get("postgres://localhost/test")
	if err then
		error(err:message())
	end
	return db
end

local function get_connection()
	return connect()
end

local db = get_connection()
local rows, err = db:query("SELECT 1")
db:release()

local function main(): boolean
	return rows ~= nil or err == nil
end

return { main = main }
