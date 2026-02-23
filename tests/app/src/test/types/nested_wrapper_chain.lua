-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function test_sql_nested_wrapper_chain(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_nested_wrapper_chain",
		kind = "function.lua",
		data = {
			source = [[
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

return { main = main }]],
			method = "main",
			modules = { "sql" },
		},
	})

	local ver, err = changes:apply()
	assert.not_nil(ver, "nested wrapper chain should preserve sql methods: " .. tostring(err))
	assert.is_nil(err, "nested wrapper chain should type-check cleanly")
	return true
end

local function main(): boolean
	test_sql_nested_wrapper_chain()
	return true
end

return { main = main }
