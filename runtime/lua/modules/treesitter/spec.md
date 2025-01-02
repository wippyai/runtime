-- Core Tree-sitter module
local treesitter = {
VERSION = "1.0",
}

-- Core functions
treesitter.parse(lang_name, source, old_tree) --> tree
treesitter.parse_query(lang_name, query_string) --> query

--[[ Tree object ]]--
-- Core operations
tree:root_node()                     -- Get root TSNode
tree:copy()                          -- Create tree copy for modifications
tree:walk()                          -- Create new tree cursor
tree:edit(edit)                      -- Apply edit to tree
tree:changed_ranges(other_tree)      -- Get ranges changed between trees

-- Rendering methods
tree:to_sexpr()                      -- Get tree structure as S-expression
tree:to_dot()                        -- Get tree as DOT graph for visualization
tree:to_code()                       -- Render back to source code string

-- Resource management
tree:close()                         -- Free tree resources

--[[ Node object ]]--
-- Navigation
node:parent()
node:child(idx)
node:child_count()
node:named_child(idx)
node:named_child_count()
node:next_sibling()
node:prev_sibling()
node:next_named_sibling()
node:prev_named_sibling()
node:child_by_field_name(name)
node:field_name_for_child(idx)

-- Inspection
node:kind()                          -- Node type as string
node:is_named()                      -- Is this a named node?
node:has_error()                     -- Does this node contain errors?
node:is_error()                      -- Is this an error node?

-- Text operations
node:text(source)                    -- Get node's text from source
node:to_code(source)                 -- Render this node back to code

-- Position
node:start_byte()                    -- Start byte offset
node:end_byte()                      -- End byte offset
node:start_point()                   -- Start position {row=N, column=N}
node:end_point()                     -- End position {row=N, column=N}

-- Example usage:
--[[
-- Original code
local source = [[
func hello() {
println("world")
}
]]

-- Parse and modify
local tree = treesitter.parse("go", source)
local new_tree = tree:copy()
new_tree:edit({
start_byte = 10,
old_end_byte = 15,
new_end_byte = 19,
start_point = {row = 0, column = 10},
old_end_point = {row = 0, column = 15},
new_end_point = {row = 0, column = 19}
})

-- Check if valid
local root = new_tree:root_node()
if not root:has_error() then
-- Get modified code
local modified_code = new_tree:to_code(source)
print(modified_code)
else
print("Invalid modification!")
end

-- Can also render specific nodes
local func_node = root:named_child(0)
print(func_node:to_code(source))  -- Just the function code

-- For debugging, can see tree structure
print(new_tree:to_sexpr())
]]