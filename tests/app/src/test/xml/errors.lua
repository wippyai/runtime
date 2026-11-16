-- SPDX-License-Identifier: MPL-2.0

-- Test: xml structured errors
local assert = require("assert2")
local xml = require("xml")

local function code_of(err: error?): string?
	if not err then
		return nil
	end
	local details = err:details()
	return details and details.code
end

local function main(): boolean
	local doc, err = xml.decode("<order>\n  <total>1</totl>\n</order>")
	assert.is_nil(doc, "no tree on error")
	assert.not_nil(err, "syntax error")
	if err then
		assert.eq(err:kind(), errors.INVALID, "kind")
		assert.eq(err:retryable(), false, "not retryable")
		local details = err:details()
		assert.eq(details.code, "syntax", "code")
		assert.eq(details.line, 2, "line")
		assert.eq(details.path[1], "order", "outer element")
		assert.eq(details.path[2], "total", "open element")
	end

	local _, limit = xml.decode("<a><b><c/></b></a>", {max_depth = 2})
	assert.eq(code_of(limit), "limit", "limit code")
	assert.eq(limit and limit:details().limit, "max_depth", "limit option")

	local _, ns = xml.decode("<p:a/>")
	assert.eq(code_of(ns), "namespace", "namespace code")

	local _, enc = xml.decode("<a>\255</a>")
	assert.eq(code_of(enc), "encoding", "encoding code")

	local unchecked: any = {whitespace = "trim"}
	local _, arg = xml.decode("<a/>", unchecked)
	assert.eq(code_of(arg), "argument", "argument code")

	local _, shape = xml.encode({name = "a", text = "x", children = {{name = "b"}}})
	assert.eq(code_of(shape), "element", "element code")

	local reader = xml.reader("<a>text</a>")
	reader:next()
	reader:next()
	local _, state = reader:node()
	assert.eq(code_of(state), "state", "state code")
	reader:close()

	local records = xml.reader("<r><i/><i></r>"):select("r/i")
	local first = records:read(1)
	assert.eq(first and #first, 1, "records before the error")
	local _, sticky1 = records:read(1)
	local _, sticky2 = records:read(1)
	assert.eq(code_of(sticky1), "syntax", "records error")
	assert.eq(sticky2 and sticky2:message(), sticky1 and sticky1:message(), "the error is sticky")
	records:close()
	return true
end

return { main = main }
