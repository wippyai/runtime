-- SPDX-License-Identifier: MPL-2.0

-- Test: Tree-sitter JavaScript language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify JS is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["javascript"], "JavaScript is supported")

	-- Test parsing JavaScript code
	local code = [[
import { useState, useEffect } from 'react';

class Calculator {
    constructor(value = 0) {
        this.value = value;
    }

    add(n) {
        return this.value + n;
    }
}

function greet(name) {
    return `Hello, ${name}!`;
}

const multiply = (a, b) => a * b;

async function fetchData(url) {
    const response = await fetch(url);
    return response.json();
}

export { Calculator, greet, multiply };
]]

	local tree = treesitter.parse("javascript", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "program", "root is program")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for function declarations
	local fn_query = treesitter.query("javascript", [[
        (function_declaration name: (identifier) @func_name)
    ]])
	local fn_captures = fn_query:captures(root, code)
	assert.eq(#fn_captures, 2, "found 2 function declarations")
	fn_query:close()

	-- Query for arrow functions
	local arrow_query = treesitter.query("javascript", [[
        (lexical_declaration
            (variable_declarator
                name: (identifier) @arrow_name
                value: (arrow_function)))
    ]])
	local arrow_captures = arrow_query:captures(root, code)
	assert.eq(#arrow_captures, 1, "found 1 arrow function")
	assert.eq(arrow_captures[1].text, "multiply", "arrow function is multiply")
	arrow_query:close()

	-- Query for class declarations
	local class_query = treesitter.query("javascript", [[
        (class_declaration name: (identifier) @class_name)
    ]])
	local class_captures = class_query:captures(root, code)
	assert.eq(#class_captures, 1, "found 1 class")
	assert.eq(class_captures[1].text, "Calculator", "class is Calculator")
	class_query:close()

	-- Query for method definitions
	local method_query = treesitter.query("javascript", [[
        (method_definition name: (property_identifier) @method_name)
    ]])
	local method_captures = method_query:captures(root, code)
	assert.eq(#method_captures, 2, "found 2 methods")
	method_query:close()

	-- Test node navigation
	local child = root:child(0)
	assert.not_nil(child, "has first child")
	assert.eq(child:kind(), "import_statement", "first child is import")

	local parent = child:parent()
	assert.not_nil(parent, "can get parent")
	assert.eq(parent:kind(), "program", "parent is program")

	-- Test cursor navigation
	local cursor = tree:walk()
	cursor:goto_first_child()
	local first_kind = cursor:current_node():kind()
	assert.eq(first_kind, "import_statement", "cursor navigates to import")

	-- Test sibling navigation
	cursor:goto_next_sibling()
	assert.eq(cursor:current_node():kind(), "class_declaration", "next sibling is class")

	cursor:close()

	-- Test with "js" alias
	local tree2 = treesitter.parse("js", "const x = 42;")
	assert.not_nil(tree2, "js alias works")
	assert.ok(not tree2:root_node():has_error(), "parses correctly")
	tree2:close()

	-- Test language object
	local lang = treesitter.language("javascript")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	tree:close()

	return true
end

return { main = main }
