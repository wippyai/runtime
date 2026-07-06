-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local archive = require("archive")
local fs = require("fs")

local function main()
	local vol, err = fs.get("app:temp")
	assert.is_nil(err, "temp fs error")

	-- unknown format from a non-archive byte source
	local r, oerr = archive.open("not an archive at all")
	assert.is_nil(r, "open garbage returns nil")
	assert.not_nil(oerr, "open garbage returns error")
	assert.eq(oerr:kind(), errors.INVALID, "unknown format is Invalid")

	-- formats() lists the built-in codecs
	local fmts = archive.formats()
	assert.is_table(fmts, "formats is a table")
	local set = {}
	for _, f in ipairs(fmts) do
		set[f] = true
	end
	assert.ok(set["zip"], "zip registered")
	assert.ok(set["tar"], "tar registered")
	assert.ok(set["tar.gz"], "tar.gz registered")
	assert.ok(set["tar.zst"], "tar.zst registered")

	-- read() guards a large entry behind max_inline_bytes; stream() still works
	local w, cerr = archive.create(vol, "/er_out.zip")
	assert.is_nil(cerr, "create error")
	assert.ok(w:add("big.txt", string.rep("x", 4096)), "add big entry")
	assert.ok(w:close(), "close writer")

	local rr, rerr = archive.open(vol, "/er_out.zip", { max_inline_bytes = 16 })
	assert.is_nil(rerr, "open error")
	local data, derr = rr:read("big.txt")
	assert.is_nil(data, "read over inline cap returns nil")
	assert.not_nil(derr, "read over inline cap returns error")
	assert.not_nil(rr:stream("big.txt"), "stream still works for large entry")
	assert.ok(rr:close(), "close reader")

	-- max_total_bytes rejects an over-budget extract_all
	local w2 = archive.create(vol, "/er_tot.zip")
	for i = 1, 3 do
		assert.ok(w2:add("f" .. i .. ".txt", string.rep("y", 1000)), "add f" .. i)
	end
	assert.ok(w2:close(), "close writer 2")

	local r2 = archive.open(vol, "/er_tot.zip", { max_total_bytes = 1500 })
	local n, terr = r2:extract_all(vol, { prefix = "er_tot/" })
	assert.is_nil(n, "over-budget extract returns nil count")
	assert.not_nil(terr, "over-budget extract returns error")
	assert.ok(r2:close(), "close reader 2")

	vol:remove("/er_out.zip")
	vol:remove("/er_tot.zip")
	return true
end

return { main = main }
