-- SPDX-License-Identifier: MPL-2.0

-- Test: Tree-sitter SQL language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify SQL is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["sql"], "SQL is supported")

	-- Test parsing SQL code (simplified for tree-sitter compatibility)
	local code = [[
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    age INTEGER
);

INSERT INTO users (name, email, age) VALUES ('Alice', 'alice@example.com', 30);

SELECT id, name, email FROM users WHERE age >= 18;

UPDATE users SET name = 'Charlie' WHERE id = 1;

DELETE FROM users WHERE age < 18;
]]

	local tree = treesitter.parse("sql", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.ok(root:kind() ~= nil, "root has kind")

	-- Query for SELECT statements (node types vary by tree-sitter-sql)
	local select_query = treesitter.query("sql", [[
        (select_statement) @select
    ]])
	if select_query then
		local select_captures = select_query:captures(root, code)
		assert.ok(#select_captures >= 1, "found SELECT statements")
		select_query:close()
	end

	-- Query for CREATE TABLE statements
	local create_query = treesitter.query("sql", [[
        (create_table_statement) @create
    ]])
	if create_query then
		local create_captures = create_query:captures(root, code)
		assert.ok(#create_captures >= 1, "found CREATE TABLE")
		create_query:close()
	end

	-- Query for INSERT statements
	local insert_query = treesitter.query("sql", [[
        (insert_statement) @insert
    ]])
	if insert_query then
		local insert_captures = insert_query:captures(root, code)
		assert.ok(#insert_captures >= 1, "found INSERT")
		insert_query:close()
	end

	-- Query for UPDATE statements
	local update_query = treesitter.query("sql", [[
        (update_statement) @update
    ]])
	if update_query then
		local update_captures = update_query:captures(root, code)
		assert.ok(#update_captures >= 1, "found UPDATE")
		update_query:close()
	end

	-- Query for DELETE statements
	local delete_query = treesitter.query("sql", [[
        (delete_statement) @delete
    ]])
	if delete_query then
		local delete_captures = delete_query:captures(root, code)
		assert.ok(#delete_captures >= 1, "found DELETE")
		delete_query:close()
	end

	-- Test simple query parsing
	local simple_tree = treesitter.parse("sql", "SELECT id FROM users")
	assert.not_nil(simple_tree, "simple query parses")
	simple_tree:close()

	-- Test cursor navigation
	local cursor = tree:walk()
	cursor:goto_first_child()

	local statement_count = 1
	while cursor:goto_next_sibling() do
		statement_count = statement_count + 1
	end
	assert.ok(statement_count >= 3, "has multiple statements")

	cursor:close()

	-- Test language object
	local lang = treesitter.language("sql")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	tree:close()

	return true
end

return { main = main }
