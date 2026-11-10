-- SPDX-License-Identifier: MPL-2.0

-- Test: xml reader copy into writers
local assert = require("assert2")
local xml = require("xml")
local fs = require("fs")

local function feed(count: integer): string
	local parts = {'<orders xmlns="urn:orders" xmlns:x="urn:ext">'}
	for i = 1, count do
		local status = i % 2 == 0 and "active" or "closed"
		parts[#parts + 1] = '<order id="' .. i .. '" status="' .. status .. '"><x:line sku="A">' .. i .. '</x:line></order>'
	end
	parts[#parts + 1] = "</orders>"
	return table.concat(parts)
end

-- filter copies active orders from reader to writer without materialising them
local function filter(reader: xml.Reader, writer: xml.Writer): integer
	local copied = 0
	assert.ok(writer:start_element("active"))
	while true do
		local kind, err = reader:next()
		assert.is_nil(err, "next error")
		if not kind then
			break
		end
		if kind == "start" and reader:name() == "order" then
			if reader:attr("status") == "active" then
				local ok, cerr = reader:copy(writer)
				assert.is_nil(cerr, "copy error")
				assert.ok(ok, "copied")
				copied = copied + 1
			else
				reader:skip()
			end
		end
	end
	assert.ok(writer:end_element())
	return copied
end

local function main(): boolean
	local document = feed(2000)

	local reader = xml.reader(document)
	local writer = xml.writer()
	local copied = filter(reader, writer)
	local out, err = writer:finish()
	assert.is_nil(err, "finish error")
	assert.eq(copied, 1000, "active orders copied")
	local filtered = xml.decode(out or "")
	local first = filtered and xml.child(filtered, "order")
	assert.eq(first and first.xmlns and first.xmlns.x, "urn:ext", "inherited namespaces are declared on copies")
	assert.eq(first and xml.attr(first, "id"), "2", "first active order")

	local vol = fs.get("app:temp")
	local base = "/xml_copy_" .. os.time()
	assert.ok(vol:writefile(base .. "_in.xml", document), "write input")
	local input = vol:open(base .. "_in.xml", "r")
	local output = vol:open(base .. "_out.xml", "w")
	local file_reader = xml.reader(input)
	local file_writer = xml.writer(output)
	assert.eq(filter(file_reader, file_writer), 1000, "file to file")
	local _, ferr = file_writer:finish()
	assert.is_nil(ferr, "finish to file")
	file_reader:close()
	file_writer:close()
	input:close()
	output:close()
	assert.eq(vol:readfile(base .. "_out.xml"), out, "streamed copy matches the in-memory copy")
	vol:remove(base .. "_in.xml")
	vol:remove(base .. "_out.xml")
	return true
end

return { main = main }
