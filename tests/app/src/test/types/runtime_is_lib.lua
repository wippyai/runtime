local assert = require("assert2")
local mylib = require("mylib")

type LocalPoint = {x: number, y: number}

local function main(): boolean
	local point, perr = LocalPoint:is({x = 1, y = 2})
	assert.not_nil(point, "LocalPoint should validate")
	assert.is_nil(perr, "LocalPoint should have nil error")

	local cfg = mylib.create("localhost", 8080)
	local cfg_check, cfg_err = mylib.Config:is(cfg)
	assert.not_nil(cfg_check, "Config from library should validate")
	assert.is_nil(cfg_err, "Config from library should have nil error")

	local bad_cfg, bad_cfg_err = mylib.Config:is({host = "localhost"})
	assert.is_nil(bad_cfg, "Config missing port should fail")
	assert.not_nil(bad_cfg_err, "Config missing port should return error")
	assert.error_contains(bad_cfg_err, "port", "Config error should mention port")

	return true
end

return { main = main }
