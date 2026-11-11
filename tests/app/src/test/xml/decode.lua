-- SPDX-License-Identifier: MPL-2.0

-- Test: xml.decode normal form and namespaces
local assert = require("assert2")
local xml = require("xml")

local function main(): boolean
	local doc, err = xml.decode([[<order id="7" xmlns="urn:shop" xmlns:x="urn:ext">
  <total currency="USD">12.50</total>
  <note>pay <b>now</b> please</note>
  <empty/>
  <x:meta x:source="api"/>
</order>]], {whitespace = "strip"})
	assert.is_nil(err, "decode error")
	assert.not_nil(doc, "document")
	if not doc then
		return false
	end

	assert.eq(doc.name, "order", "root name")
	assert.eq(doc.ns, "urn:shop", "root namespace")
	assert.eq(doc.attrs and doc.attrs.id, "7", "attribute values are strings")
	assert.eq(doc.xmlns and doc.xmlns[""], "urn:shop", "default declaration")
	assert.eq(doc.xmlns and doc.xmlns.x, "urn:ext", "prefixed declaration")
	assert.is_nil(doc.text, "element content has no text field")

	local children = doc.children or {}
	assert.eq(#children, 4, "whitespace between elements is stripped")

	local total = xml.child(doc, "total")
	assert.eq(total and total.text, "12.50", "numbers are never inferred")
	assert.eq(total and total.ns, "urn:shop", "default namespace applies to elements")

	local note = xml.child(doc, "note")
	local parts = note and note.children or {}
	assert.eq(#parts, 3, "mixed content keeps document order")
	assert.eq(parts[1], "pay ", "leading text")
	assert.eq(parts[3], " please", "trailing text")

	local empty = xml.child(doc, "empty")
	assert.not_nil(empty, "empty element")
	assert.is_nil(empty and empty.text, "an empty element has no text")
	assert.is_nil(empty and empty.children, "an empty element has no children")

	local meta = xml.child(doc, "meta", "urn:ext")
	assert.eq(xml.attr(meta or doc, "source", "urn:ext"), "api", "qualified attribute")
	assert.is_nil(meta and meta.xmlns, "descendants carry only their own declarations")

	local cdata = xml.decode("<a>x<![CDATA[<y>]]>&amp;z<!-- gone --></a>")
	assert.eq(cdata and cdata.text, "x<y>&z", "CDATA and references become text")

	local preserved = xml.decode("<a> <b/> </a>")
	assert.eq(preserved and preserved.children and #preserved.children, 3, "whitespace is preserved by default")
	return true
end

return { main = main }
