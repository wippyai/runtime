-- SPDX-License-Identifier: MPL-2.0

-- Test: xml typed element handling
local assert = require("assert2")
local xml = require("xml")

type Book = {id: string, title: string, tags: {string}}

-- collect returns the text of direct text children and the names of child elements
local function collect(element: xml.Element): ({string}, {string})
	local texts: {string} = {}
	local names: {string} = {}
	for _, child in ipairs(element.children or {}) do
		if type(child) == "string" then
			texts[#texts + 1] = child
		else
			names[#names + 1] = child.name
		end
	end
	return texts, names
end

local function to_book(element: xml.Element): Book
	local tags: {string} = {}
	for _, tag in ipairs(xml.children(element, "tag")) do
		tags[#tags + 1] = xml.text(tag)
	end
	local title = xml.child(element, "title")
	return {
		id = xml.attr(element, "id") or "",
		title = title and xml.text(title) or "",
		tags = tags,
	}
end

local function build(books: {Book}): xml.Element
	local children: {string | xml.Element} = {}
	for _, book in ipairs(books) do
		local tags: {string | xml.Element} = {}
		for _, tag in ipairs(book.tags) do
			tags[#tags + 1] = {name = "tag", text = tag}
		end
		children[#children + 1] = {
			name = "book",
			attrs = {id = book.id},
			children = {{name = "title", text = book.title}, table.unpack(tags)},
		}
	end
	return {name = "library", children = children}
end

local function main(): boolean
	local doc, err = xml.decode('<library><book id="1"><title>Go</title><tag>lang</tag></book>note<book id="2"><title>Lua</title></book></library>')
	assert.is_nil(err, "decode error")
	if not doc then
		return false
	end

	local texts, names = collect(doc)
	assert.eq(table.concat(texts, ","), "note", "text children")
	assert.eq(table.concat(names, ","), "book,book", "element children")

	local books: {Book} = {}
	for _, element in ipairs(xml.children(doc, "book")) do
		books[#books + 1] = to_book(element)
	end
	assert.eq(#books, 2, "books")
	assert.eq(books[1].tags[1], "lang", "tags")

	local rebuilt = xml.encode(build(books))
	assert.eq(rebuilt, '<library><book id="1"><title>Go</title><tag>lang</tag></book>' ..
		'<book id="2"><title>Lua</title></book></library>', "typed tree encodes")
	return true
end

return { main = main }
