-- Test: Tree-sitter Lua language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify Lua is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["lua"], "Lua is supported")

	-- Test parsing Lua code
	local code = [[
local Module = {}

local private_var = 42

function Module.new(name)
    local self = setmetatable({}, { __index = Module })
    self.name = name
    self.items = {}
    return self
end

function Module:add(item)
    table.insert(self.items, item)
    return self
end

function Module:get_count()
    return #self.items
end

local function helper(x, y)
    return x + y
end

for i = 1, 10 do
    print(i)
end

if true then
    print("yes")
elseif false then
    print("no")
end

return Module
]]

	local tree = treesitter.parse("lua", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "chunk", "root is chunk")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for function declarations (global style)
	local fn_query = treesitter.query("lua", [[
        (function_declaration name: (identifier) @func_name)
        (function_declaration name: (dot_index_expression) @method_name)
        (function_declaration name: (method_index_expression) @method_name)
    ]])
	local fn_captures = fn_query:captures(root, code)
	assert.ok(#fn_captures >= 4, "found function declarations")
	fn_query:close()

	-- Query for identifiers (simpler test)
	local id_query = treesitter.query("lua", [[
        (identifier) @id
    ]])
	local id_captures = id_query:captures(root, code)
	assert.ok(#id_captures >= 5, "found identifiers")
	id_query:close()

	-- Query for for statements
	local loop_query = treesitter.query("lua", [[
        (for_statement) @for_loop
    ]])
	if loop_query then
		local loop_captures = loop_query:captures(root, code)
		assert.ok(#loop_captures >= 1, "found for loop")
		loop_query:close()
	end

	-- Query for if statements
	local if_query = treesitter.query("lua", [[
        (if_statement) @if_stmt
    ]])
	if if_query then
		local if_captures = if_query:captures(root, code)
		assert.ok(#if_captures >= 1, "found if statement")
		if_query:close()
	end

	-- Test node text extraction
	local first_child = root:child(0)
	assert.not_nil(first_child, "has first child")
	local text = first_child:text()
	assert.ok(text:find("Module") ~= nil, "first statement contains Module")

	-- Test cursor navigation
	local cursor = tree:walk()
	assert.eq(cursor:current_depth(), 0, "starts at depth 0")

	cursor:goto_first_child()
	assert.eq(cursor:current_depth(), 1, "descended to depth 1")

	local desc_idx = cursor:current_descendant_index()
	assert.ok(desc_idx >= 0, "has descendant index")

	cursor:close()

	-- Test language object
	local lang = treesitter.language("lua")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	-- Test specific node kind
	local chunk_id = lang:id_for_node_kind("chunk", true)
	assert.ok(chunk_id > 0, "chunk has an id")
	local kind_name = lang:node_kind_for_id(chunk_id)
	assert.eq(kind_name, "chunk", "can get kind name from id")

	tree:close()

	return true
end

return { main = main }
