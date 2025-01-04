local treesitter = require("treesitter")

-- Initial code to parse
local code = "package main; func main() { x := 42 }"

-- Create parser and parse initial code
local parser = treesitter.parser()
assert(parser:set_language("go"), "Failed to set language")
local tree = parser:parse(code)
assert(tree ~= nil, "Initial parse failed")

-- Find the number using position
local root = tree:root_node()

-- Edit: replace "42" with "100"
local edit = {
    start_byte = 30,  -- Position of "42"
    old_end_byte = 32, -- End of "42"
    new_end_byte = 33, -- End position after "100"
    start_row = 0,
    start_column = 30,
    old_end_row = 0,
    old_end_column = 32,
    new_end_row = 0,
    new_end_column = 33
}

-- Apply edit to tree
assert(tree:edit(edit), "Edit should succeed")

-- New code with edit
local new_code = "package main; func main() { x := 100 }"

-- Reset parser and do new parse with reference to old tree
parser:reset()
local new_tree = parser:parse(new_code, tree)
assert(new_tree ~= nil, "Reparse failed")

-- Get changed ranges
local ranges = tree:changed_ranges(new_tree)
assert(#ranges > 0, "Should detect changes")