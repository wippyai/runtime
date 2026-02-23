-- SPDX-License-Identifier: MPL-2.0

-- Test: Tree-sitter Python language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify Python is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["python"], "Python is supported")

	-- Test parsing Python code
	local code = [[
import os
from typing import List, Optional

class User:
    """A user class with name and age."""

    def __init__(self, name: str, age: int):
        self.name = name
        self.age = age

    def greet(self) -> str:
        return f"Hello, {self.name}!"

    @property
    def is_adult(self) -> bool:
        return self.age >= 18

def process_users(users: List[User]) -> None:
    for user in users:
        print(user.greet())

async def fetch_data(url: str) -> Optional[dict]:
    pass

if __name__ == "__main__":
    user = User("Alice", 30)
    print(user.greet())
]]

	local tree = treesitter.parse("python", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "module", "root is module")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for function definitions
	local fn_query = treesitter.query("python", [[
        (function_definition name: (identifier) @func_name)
    ]])
	local fn_captures = fn_query:captures(root, code)
	assert.ok(#fn_captures >= 4, "found functions (including methods)")
	fn_query:close()

	-- Query for class definitions
	local class_query = treesitter.query("python", [[
        (class_definition name: (identifier) @class_name)
    ]])
	local class_captures = class_query:captures(root, code)
	assert.eq(#class_captures, 1, "found 1 class")
	assert.eq(class_captures[1].text, "User", "class is User")
	class_query:close()

	-- Query for decorators
	local deco_query = treesitter.query("python", [[
        (decorator (identifier) @decorator_name)
    ]])
	local deco_captures = deco_query:captures(root, code)
	assert.eq(#deco_captures, 1, "found 1 decorator")
	assert.eq(deco_captures[1].text, "property", "decorator is property")
	deco_query:close()

	-- Query for imports
	local import_query = treesitter.query("python", [[
        (import_statement name: (dotted_name) @import_name)
        (import_from_statement module_name: (dotted_name) @from_module)
    ]])
	local import_captures = import_query:captures(root, code)
	assert.eq(#import_captures, 2, "found 2 imports")
	import_query:close()

	-- Test node navigation
	local cursor = tree:walk()
	cursor:goto_first_child()

	-- Navigate through children
	local count = 0
	repeat
		count = count + 1
	until not cursor:goto_next_sibling()
	assert.ok(count > 3, "has multiple top-level statements")

	cursor:close()

	-- Test with "py" alias
	local tree2 = treesitter.parse("py", "x = 42")
	assert.not_nil(tree2, "py alias works")
	assert.ok(not tree2:root_node():has_error(), "parses correctly")
	tree2:close()

	-- Test language object
	local lang = treesitter.language("python")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:node_kind_count() > 50, "has many node kinds")

	tree:close()

	return true
end

return { main = main }
