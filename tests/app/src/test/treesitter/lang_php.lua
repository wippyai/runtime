-- Test: Tree-sitter PHP language support
local assert = require("assert_primitives")

local function main()
    local treesitter = require("treesitter")

    -- Verify PHP is in supported languages
    local langs = treesitter.supported_languages()
    assert.ok(langs["php"], "PHP is supported")

    -- Test parsing PHP code
    local code = [[
<?php

namespace App\Models;

use App\Contracts\UserInterface;
use App\Traits\HasTimestamps;

class User implements UserInterface
{
    use HasTimestamps;

    private string $name;
    private int $age;
    public readonly string $id;

    public function __construct(string $name, int $age)
    {
        $this->name = $name;
        $this->age = $age;
        $this->id = uniqid();
    }

    public function getName(): string
    {
        return $this->name;
    }

    public function isAdult(): bool
    {
        return $this->age >= 18;
    }

    public static function create(array $data): self
    {
        return new self($data['name'], $data['age']);
    }
}

function helper(mixed $value): void
{
    var_dump($value);
}

$user = new User("Alice", 30);
echo $user->getName();
?>
]]

    local tree = treesitter.parse("php", code)
    assert.not_nil(tree, "parse returns tree")

    local root = tree:root_node()
    assert.eq(root:kind(), "program", "root is program")
    assert.ok(not root:has_error(), "no parse errors")

    -- Query for class declarations
    local class_query = treesitter.query("php", [[
        (class_declaration name: (name) @class_name)
    ]])
    local class_captures = class_query:captures(root, code)
    assert.eq(#class_captures, 1, "found 1 class")
    assert.eq(class_captures[1].text, "User", "class is User")
    class_query:close()

    -- Query for methods
    local method_query = treesitter.query("php", [[
        (method_declaration name: (name) @method_name)
    ]])
    local method_captures = method_query:captures(root, code)
    assert.eq(#method_captures, 4, "found 4 methods")
    method_query:close()

    -- Query for function declarations
    local fn_query = treesitter.query("php", [[
        (function_definition name: (name) @func_name)
    ]])
    local fn_captures = fn_query:captures(root, code)
    assert.eq(#fn_captures, 1, "found 1 function")
    assert.eq(fn_captures[1].text, "helper", "function is helper")
    fn_query:close()

    -- Query for namespace
    local ns_query = treesitter.query("php", [[
        (namespace_definition name: (namespace_name) @ns_name)
    ]])
    local ns_captures = ns_query:captures(root, code)
    assert.eq(#ns_captures, 1, "found 1 namespace")
    ns_query:close()

    -- Query for use statements
    local use_query = treesitter.query("php", [[
        (namespace_use_declaration (namespace_use_clause (qualified_name) @use_name))
    ]])
    local use_captures = use_query:captures(root, code)
    assert.eq(#use_captures, 2, "found 2 use statements")
    use_query:close()

    -- Query for properties
    local prop_query = treesitter.query("php", [[
        (property_declaration (property_element (variable_name) @prop_name))
    ]])
    local prop_captures = prop_query:captures(root, code)
    assert.eq(#prop_captures, 3, "found 3 properties")
    prop_query:close()

    -- Test cursor navigation
    local cursor = tree:walk()
    cursor:goto_first_child()
    assert.ok(cursor:current_depth() > 0, "descended into tree")

    -- Navigate through siblings
    local sibling_count = 1
    while cursor:goto_next_sibling() do
        sibling_count = sibling_count + 1
    end
    assert.ok(sibling_count >= 1, "has siblings")

    cursor:close()

    -- Test language object
    local lang = treesitter.language("php")
    assert.not_nil(lang, "language object created")
    assert.ok(lang:version() > 0, "has version")

    tree:close()

    return true
end

return { main = main }
