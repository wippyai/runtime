-- SPDX-License-Identifier: MPL-2.0

-- Test: UUID parsing and info
local assert = require("assert2")
local uuid = require("uuid")

local function main()
-- Test parse v1 UUID
	local info, err = uuid.parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	assert.is_nil(err, "parse v1 no error")
	assert.not_nil(info, "parse v1 returns info")
	assert.eq(info.version, 1, "v1 version")
	assert.eq(info.variant, "RFC4122", "v1 variant")
	assert.not_nil(info.timestamp, "v1 has timestamp")
	assert.not_nil(info.node, "v1 has node")

	-- Test parse v4 UUID
	local v4, _ = uuid.v4()
	local info4, err4 = uuid.parse(v4)
	assert.is_nil(err4, "parse v4 no error")
	assert.eq(info4.version, 4, "v4 version")
	assert.eq(info4.variant, "RFC4122", "v4 variant")
	assert.is_nil(info4.timestamp, "v4 no timestamp")

	-- Test parse v7 UUID
	local v7, _ = uuid.v7()
	local info7, err7 = uuid.parse(v7)
	assert.is_nil(err7, "parse v7 no error")
	assert.eq(info7.version, 7, "v7 version")
	assert.eq(info7.variant, "RFC4122", "v7 variant")
	assert.not_nil(info7.timestamp, "v7 has timestamp")

	-- Test version function
	local ver, _ = uuid.version("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	assert.eq(ver, 1, "version returns 1")

	local ver4, _ = uuid.version(v4)
	assert.eq(ver4, 4, "version returns 4")

	local ver7, _ = uuid.version(v7)
	assert.eq(ver7, 7, "version returns 7")

	-- Test variant function
	local var, _ = uuid.variant("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	assert.eq(var, "RFC4122", "variant returns RFC4122")

	return true
end

return { main = main }
