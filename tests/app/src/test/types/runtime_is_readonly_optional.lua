-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

type ReadonlyList = readonly {number}
type ReadonlyMap = readonly {[string]: number}
type Config = {id: string, tags?: readonly {string}, meta?: readonly {[string]: string}}

local function main(): boolean
	local list_ok, list_err = ReadonlyList:is({1, 2, 3})
	assert.not_nil(list_ok, "ReadonlyList valid should pass")
	assert.is_nil(list_err, "ReadonlyList valid should have nil error")

	local list_bad, list_bad_err = ReadonlyList:is({"x"})
	assert.is_nil(list_bad, "ReadonlyList wrong element type should fail")
	assert.not_nil(list_bad_err, "ReadonlyList wrong element type should return error")
	assert.error_contains(list_bad_err, "number", "ReadonlyList error should mention number")

	local map_ok, map_err = ReadonlyMap:is({a = 1, b = 2})
	assert.not_nil(map_ok, "ReadonlyMap valid should pass")
	assert.is_nil(map_err, "ReadonlyMap valid should have nil error")

	local map_bad, map_bad_err = ReadonlyMap:is({a = "x"})
	assert.is_nil(map_bad, "ReadonlyMap wrong value type should fail")
	assert.not_nil(map_bad_err, "ReadonlyMap wrong value type should return error")
	assert.error_contains(map_bad_err, "number", "ReadonlyMap error should mention number")

	local cfg_ok, cfg_err = Config:is({id = "cfg1", tags = {"a"}, meta = {env = "dev"}})
	assert.not_nil(cfg_ok, "Config valid should pass")
	assert.is_nil(cfg_err, "Config valid should have nil error")

	local cfg_ok2, cfg_err2 = Config:is({id = "cfg1"})
	assert.not_nil(cfg_ok2, "Config missing optional fields should pass")
	assert.is_nil(cfg_err2, "Config missing optional fields should have nil error")

	local cfg_bad, cfg_bad_err = Config:is({id = "cfg1", tags = {1}})
	assert.is_nil(cfg_bad, "Config tags wrong element type should fail")
	assert.not_nil(cfg_bad_err, "Config tags wrong element type should return error")
	assert.error_contains(cfg_bad_err, "string", "Config tags error should mention string")

	local cfg_bad2, cfg_bad2_err = Config:is({id = "cfg1", meta = {env = 1}})
	assert.is_nil(cfg_bad2, "Config meta wrong value type should fail")
	assert.not_nil(cfg_bad2_err, "Config meta wrong value type should return error")
	assert.error_contains(cfg_bad2_err, "string", "Config meta error should mention string")

	return true
end

return { main = main }
