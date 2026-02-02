-- Test: Tree-sitter C# language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify C# is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["c#"], "C# is supported")

	-- Test parsing C# code
	local code = [[
using System;
using System.Collections.Generic;
using System.Threading.Tasks;

namespace MyApp.Models
{
    public interface IUser
    {
        string Name { get; }
        int Age { get; }
    }

    public class User : IUser
    {
        public string Name { get; private set; }
        public int Age { get; private set; }
        public string? Email { get; set; }

        public User(string name, int age)
        {
            Name = name;
            Age = age;
        }

        public bool IsAdult() => Age >= 18;

        public async Task<string> GetGreetingAsync()
        {
            await Task.Delay(100);
            return $"Hello, {Name}!";
        }
    }

    public record Person(string Name, int Age);

    public static class UserExtensions
    {
        public static string GetDisplayName(this IUser user)
        {
            return $"{user.Name} ({user.Age})";
        }
    }
}
]]

	local tree = treesitter.parse("csharp", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "compilation_unit", "root is compilation_unit")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for class declarations
	local class_query = treesitter.query("csharp", [[
        (class_declaration name: (identifier) @class_name)
    ]])
	local class_captures = class_query:captures(root, code)
	assert.eq(#class_captures, 2, "found 2 classes")
	class_query:close()

	-- Query for interface declarations
	local iface_query = treesitter.query("csharp", [[
        (interface_declaration name: (identifier) @iface_name)
    ]])
	local iface_captures = iface_query:captures(root, code)
	assert.eq(#iface_captures, 1, "found 1 interface")
	assert.eq(iface_captures[1].text, "IUser", "interface is IUser")
	iface_query:close()

	-- Query for record declarations
	local record_query = treesitter.query("csharp", [[
        (record_declaration name: (identifier) @record_name)
    ]])
	local record_captures = record_query:captures(root, code)
	assert.eq(#record_captures, 1, "found 1 record")
	assert.eq(record_captures[1].text, "Person", "record is Person")
	record_query:close()

	-- Query for methods
	local method_query = treesitter.query("csharp", [[
        (method_declaration name: (identifier) @method_name)
    ]])
	local method_captures = method_query:captures(root, code)
	assert.eq(#method_captures, 3, "found 3 methods")
	method_query:close()

	-- Query for properties
	local prop_query = treesitter.query("csharp", [[
        (property_declaration name: (identifier) @prop_name)
    ]])
	local prop_captures = prop_query:captures(root, code)
	assert.eq(#prop_captures, 5, "found 5 properties")
	prop_query:close()

	-- Query for namespace
	local ns_query = treesitter.query("csharp", [[
        (namespace_declaration name: (qualified_name) @ns_name)
    ]])
	local ns_captures = ns_query:captures(root, code)
	assert.eq(#ns_captures, 1, "found 1 namespace")
	ns_query:close()

	-- Query for using directives
	local using_query = treesitter.query("csharp", [[
        (using_directive) @using
    ]])
	local using_captures = using_query:captures(root, code)
	assert.ok(#using_captures >= 2, "found using directives")
	using_query:close()

	-- Test with aliases
	local tree2 = treesitter.parse("cs", "class Foo {}")
	assert.not_nil(tree2, "cs alias works")
	tree2:close()

	-- Test language object
	local lang = treesitter.language("csharp")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	tree:close()

	return true
end

return { main = main }
