-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local archive = require("archive")
local fs = require("fs")

local function main()
	local vol, err = fs.get("app:temp")
	assert.is_nil(err, "temp fs error")

	local w, cerr = archive.create(vol, "/sc_out.zip")
	assert.is_nil(cerr, "create error")
	for i = 1, 3 do
		assert.ok(w:add("f" .. i .. ".txt", "data" .. i), "add f" .. i)
	end
	assert.ok(w:close(), "close writer")

	local bytes, berr = vol:readfile("/sc_out.zip")
	assert.is_nil(berr, "read zip bytes error")
	assert.is_string(bytes, "zip bytes is string")

	-- forward-only scan over an in-memory byte source
	local s, serr = archive.scan(bytes, { format = "zip" })
	assert.is_nil(serr, "scan error")
	local count = 0
	for e in s:walk() do
		assert.is_string(e.name, "entry has name")
		count = count + 1
	end
	assert.eq(count, 3, "walked entry count")
	assert.ok(s:close(), "close walker")

	-- scan + extract_all (streaming to fs, no random access)
	local s2, s2err = archive.scan(bytes, { format = "zip" })
	assert.is_nil(s2err, "second scan error")
	local n, xerr = s2:extract_all(vol, { prefix = "sc_out/" })
	assert.is_nil(xerr, "extract_all error")
	assert.eq(n, 3, "extracted count")
	assert.ok(s2:close(), "close walker 2")

	assert.eq(vol:readfile("sc_out/f1.txt"), "data1", "extracted f1")
	assert.eq(vol:readfile("sc_out/f3.txt"), "data3", "extracted f3")

	vol:remove("/sc_out.zip")
	return true
end

return { main = main }
