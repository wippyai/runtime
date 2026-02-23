-- SPDX-License-Identifier: MPL-2.0

-- Test: Tree-sitter Go language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify Go is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["go"], "Go is supported")

	-- Test parsing Go code
	local code = [[
package main

import "fmt"

type User struct {
    Name string
    Age  int
}

func (u *User) Greet() string {
    return fmt.Sprintf("Hello, %s", u.Name)
}

func main() {
    user := &User{Name: "Alice", Age: 30}
    fmt.Println(user.Greet())
}
]]

	local tree = treesitter.parse("go", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.not_nil(root, "root_node exists")
	assert.eq(root:kind(), "source_file", "root is source_file")
	assert.ok(not root:has_error(), "no parse errors")
	assert.ok(root:is_named(), "root is named")

	-- Test byte positions
	assert.eq(root:start_byte(), 0, "starts at 0")
	assert.ok(root:end_byte() > 0, "has content")

	-- Test point positions
	local start_pt = root:start_point()
	assert.eq(start_pt.row, 0, "starts at row 0")
	assert.eq(start_pt.column, 0, "starts at column 0")

	-- Query for functions
	local fn_query = treesitter.query("go", [[
        (function_declaration name: (identifier) @func_name)
        (method_declaration name: (field_identifier) @method_name)
    ]])
	assert.not_nil(fn_query, "query created")

	local captures = fn_query:captures(root, code)
	assert.eq(#captures, 2, "found 2 functions/methods")

	-- Verify capture details
	for _, cap in ipairs(captures) do
		assert.not_nil(cap.node, "capture has node")
		assert.not_nil(cap.text, "capture has text")
		assert.not_nil(cap.name, "capture has name")
	end

	fn_query:close()

	-- Query for types
	local type_query = treesitter.query("go", [[
        (type_declaration (type_spec name: (type_identifier) @type_name))
    ]])
	local type_captures = type_query:captures(root, code)
	assert.eq(#type_captures, 1, "found 1 type")
	assert.eq(type_captures[1].text, "User", "type is User")
	type_query:close()

	-- Query for imports
	local import_query = treesitter.query("go", [[
        (import_declaration (import_spec path: (interpreted_string_literal) @import))
    ]])
	local import_captures = import_query:captures(root, code)
	assert.eq(#import_captures, 1, "found 1 import")
	import_query:close()

	-- Test tree cursor
	local cursor = tree:walk()
	assert.not_nil(cursor, "cursor created")
	assert.eq(cursor:current_depth(), 0, "starts at depth 0")

	local node = cursor:current_node()
	assert.eq(node:kind(), "source_file", "cursor at root")

	-- Navigate down
	assert.ok(cursor:goto_first_child(), "has first child")
	assert.eq(cursor:current_depth(), 1, "depth is 1")

	-- Navigate back up
	assert.ok(cursor:goto_parent(), "can go to parent")
	assert.eq(cursor:current_depth(), 0, "back to depth 0")

	cursor:close()

	-- Test language object
	local lang = treesitter.language("go")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")
	assert.ok(lang:node_kind_count() > 0, "has node kinds")
	assert.ok(lang:field_count() > 0, "has fields")

	-- Test parser object with explicit lifecycle
	local parser = treesitter.parser()
	assert.not_nil(parser, "parser created")

	local ok = parser:set_language("go")
	assert.ok(ok, "set_language succeeds")
	assert.eq(parser:get_language(), "go", "language is go")

	local tree2 = parser:parse("package test\nfunc foo() {}")
	assert.not_nil(tree2, "parser can parse")
	assert.ok(not tree2:root_node():has_error(), "parsed without errors")

	parser:reset()
	parser:close()

	-- Test tree copy
	local copy = tree:copy()
	assert.not_nil(copy, "tree copied")
	assert.eq(copy:root_node():kind(), "source_file", "copy has correct root")
	copy:close()

	-- Test tree language
	local tree_lang = tree:language()
	assert.not_nil(tree_lang, "tree has language")

	tree:close()

	return true
end

return { main = main }
