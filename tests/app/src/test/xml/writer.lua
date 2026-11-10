-- SPDX-License-Identifier: MPL-2.0

-- Test: xml writer in memory and to files
local assert = require("assert2")
local xml = require("xml")
local fs = require("fs")

local function write_report(w: xml.Writer, rows: integer)
	assert.ok(w:start_element("report", {version = "1"}, "urn:report"))
	assert.ok(w:comment(" generated "))
	for i = 1, rows do
		local ok, err = w:element("row", {n = tostring(i)}, "value " .. i, "urn:report")
		assert.is_nil(err, "row error")
		assert.ok(ok, "row written")
	end
	assert.ok(w:node({name = "footer", ns = "urn:report", children = {{name = "total", ns = "urn:report", text = tostring(rows)}}}))
	assert.ok(w:end_element())
end

local function main(): boolean
	local memory, err = xml.writer(nil, {indent = "  "})
	assert.is_nil(err, "writer error")
	if not memory then
		return false
	end
	write_report(memory, 2)
	local doc, ferr = memory:finish()
	assert.is_nil(ferr, "finish error")
	assert.eq(doc, '<report xmlns="urn:report" version="1"><!-- generated -->\n' ..
		'  <row n="1">value 1</row>\n  <row n="2">value 2</row>\n' ..
		'  <footer>\n    <total>2</total>\n  </footer>\n</report>', "in-memory document")
	assert.ok(memory:close(), "close")

	local incomplete = xml.writer()
	if incomplete then
		incomplete:start_element("open")
		local _, ierr = incomplete:finish()
		assert.eq(ierr and ierr:details().code, "state", "finish with open elements")
		incomplete:close()
	end

	local vol = fs.get("app:temp")
	local path = "/xml_writer_" .. os.time() .. ".xml"
	local file = vol:open(path, "w")
	local stream_writer = xml.writer(file, {declaration = true})
	assert.not_nil(stream_writer, "file writer")
	if stream_writer then
		write_report(stream_writer, 5000)
		local result, serr = stream_writer:finish()
		assert.is_nil(serr, "finish to file")
		assert.is_nil(result, "a stream writer returns no document")
		stream_writer:close()
	end
	file:close()

	local written = vol:readfile(path)
	local parsed, perr = xml.decode(written)
	assert.is_nil(perr, "written file is well formed")
	assert.eq(parsed and #xml.children(parsed, "row"), 5000, "all rows written")
	vol:remove(path)
	return true
end

return { main = main }
