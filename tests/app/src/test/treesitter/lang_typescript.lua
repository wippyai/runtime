-- Test: Tree-sitter TypeScript language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify TypeScript is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["typescript"], "TypeScript is supported")
	assert.ok(langs["typescript+jsx"], "TSX is supported")

	-- Test parsing TypeScript code
	local code = [[
interface User {
    id: number;
    name: string;
    email?: string;
}

type Status = "active" | "inactive" | "pending";

class UserService {
    private users: Map<number, User> = new Map();

    constructor(private readonly apiUrl: string) {}

    async getUser(id: number): Promise<User | null> {
        return this.users.get(id) ?? null;
    }

    addUser(user: User): void {
        this.users.set(user.id, user);
    }
}

function processUser<T extends User>(user: T): string {
    return user.name;
}

const createUser = (name: string, id: number): User => ({
    id,
    name,
});

export { UserService, processUser, createUser };
export type { User, Status };
]]

	local tree = treesitter.parse("typescript", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "program", "root is program")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for interfaces
	local iface_query = treesitter.query("typescript", [[
        (interface_declaration name: (type_identifier) @iface_name)
    ]])
	local iface_captures = iface_query:captures(root, code)
	assert.eq(#iface_captures, 1, "found 1 interface")
	assert.eq(iface_captures[1].text, "User", "interface is User")
	iface_query:close()

	-- Query for type aliases
	local type_query = treesitter.query("typescript", [[
        (type_alias_declaration name: (type_identifier) @type_name)
    ]])
	local type_captures = type_query:captures(root, code)
	assert.eq(#type_captures, 1, "found 1 type alias")
	assert.eq(type_captures[1].text, "Status", "type is Status")
	type_query:close()

	-- Query for class declarations
	local class_query = treesitter.query("typescript", [[
        (class_declaration name: (type_identifier) @class_name)
    ]])
	local class_captures = class_query:captures(root, code)
	assert.eq(#class_captures, 1, "found 1 class")
	assert.eq(class_captures[1].text, "UserService", "class is UserService")
	class_query:close()

	-- Query for methods
	local method_query = treesitter.query("typescript", [[
        (method_definition name: (property_identifier) @method_name)
    ]])
	local method_captures = method_query:captures(root, code)
	assert.ok(#method_captures >= 2, "found methods")
	method_query:close()

	-- Query for function declarations
	local fn_query = treesitter.query("typescript", [[
        (function_declaration name: (identifier) @func_name)
    ]])
	local fn_captures = fn_query:captures(root, code)
	assert.eq(#fn_captures, 1, "found 1 function declaration")
	assert.eq(fn_captures[1].text, "processUser", "function is processUser")
	fn_query:close()

	-- Test with "ts" alias
	local tree2 = treesitter.parse("ts", "const x: number = 42;")
	assert.not_nil(tree2, "ts alias works")
	assert.ok(not tree2:root_node():has_error(), "parses correctly")
	tree2:close()

	-- Test TSX parsing
	local tsx_code = [[
import React from 'react';

interface Props {
    name: string;
}

const Greeting: React.FC<Props> = ({ name }) => {
    return <div className="greeting">Hello, {name}!</div>;
};

export default Greeting;
]]

	local tsx_tree = treesitter.parse("tsx", tsx_code)
	assert.not_nil(tsx_tree, "TSX parses")
	assert.ok(not tsx_tree:root_node():has_error(), "TSX no parse errors")

	-- Query for JSX elements in TSX
	local jsx_query = treesitter.query("tsx", [[
        (jsx_element open_tag: (jsx_opening_element name: (identifier) @tag_name))
    ]])
	local jsx_captures = jsx_query:captures(tsx_tree:root_node(), tsx_code)
	assert.eq(#jsx_captures, 1, "found 1 JSX element")
	assert.eq(jsx_captures[1].text, "div", "element is div")
	jsx_query:close()

	tsx_tree:close()

	-- Test language object
	local lang = treesitter.language("typescript")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	tree:close()

	return true
end

return { main = main }
