-- Test: Tree-sitter resource management and cleanup
local assert = require("assert_primitives")

local function main()
    local treesitter = require("treesitter")

    -- Test: Parser lifecycle - explicit close
    local parser = treesitter.parser()
    assert.not_nil(parser, "parser created")
    parser:set_language("go")
    local tree = parser:parse("package main")
    assert.not_nil(tree, "parser produces tree")
    tree:close()
    parser:close()

    -- Test: Double close on parser should not crash
    parser:close()

    -- Test: Tree lifecycle - explicit close
    local tree2 = treesitter.parse("go", "package main")
    assert.not_nil(tree2, "tree created")
    tree2:close()

    -- Test: Double close on tree should not crash
    tree2:close()

    -- Test: Query lifecycle - explicit close
    local query = treesitter.query("go", "(identifier) @id")
    assert.not_nil(query, "query created")
    query:close()

    -- Test: Double close on query should not crash
    query:close()

    -- Test: Cursor lifecycle - explicit close
    local tree3 = treesitter.parse("go", "package main\nfunc foo() {}")
    local cursor = tree3:walk()
    assert.not_nil(cursor, "cursor created")
    cursor:goto_first_child()
    cursor:close()

    -- Test: Double close on cursor should not crash
    cursor:close()
    tree3:close()

    -- Test: Tree copy lifecycle
    local original = treesitter.parse("go", "package main")
    local copy = original:copy()
    assert.not_nil(copy, "tree copied")

    -- Close copy first
    copy:close()
    copy:close()  -- Double close should not crash

    -- Close original
    original:close()
    original:close()  -- Double close should not crash

    -- Test: Cursor copy lifecycle
    local tree4 = treesitter.parse("go", "package main")
    local cursor1 = tree4:walk()
    cursor1:goto_first_child()
    local cursor2 = cursor1:copy()
    assert.not_nil(cursor2, "cursor copied")

    -- Close both cursors
    cursor1:close()
    cursor2:close()
    cursor1:close()  -- Double close should not crash
    cursor2:close()  -- Double close should not crash
    tree4:close()

    -- Test: Resources created without explicit close
    -- These should be cleaned up by the resource store when frame ends
    for i = 1, 10 do
        local t = treesitter.parse("go", "package main\nvar x = " .. i)
        local r = t:root_node()
        assert.ok(not r:has_error(), "parse " .. i .. " succeeded")
        -- Intentionally not closing - resource store should handle it
    end

    -- Test: Many parsers without explicit close
    for i = 1, 5 do
        local p = treesitter.parser()
        p:set_language("javascript")
        local t = p:parse("const x = " .. i)
        assert.not_nil(t, "parser " .. i .. " produced tree")
        -- Intentionally not closing
    end

    -- Test: Many queries without explicit close
    for i = 1, 5 do
        local q = treesitter.query("python", "(identifier) @id")
        assert.not_nil(q, "query " .. i .. " created")
        -- Intentionally not closing
    end

    -- Test: Many cursors without explicit close
    local tree5 = treesitter.parse("lua", "local x = 1\nlocal y = 2\nlocal z = 3")
    for i = 1, 5 do
        local c = tree5:walk()
        c:goto_first_child()
        for j = 1, i do
            c:goto_next_sibling()
        end
        assert.ok(c:current_depth() >= 0, "cursor " .. i .. " navigated")
        -- Intentionally not closing
    end
    tree5:close()

    -- Test: Interleaved resource creation
    local resources = {}
    for i = 1, 3 do
        local t = treesitter.parse("go", "package p" .. i)
        local q = treesitter.query("go", "(package_clause) @pkg")
        local c = t:walk()
        table.insert(resources, {tree = t, query = q, cursor = c})
    end

    -- Close in reverse order
    for i = #resources, 1, -1 do
        resources[i].cursor:close()
        resources[i].query:close()
        resources[i].tree:close()
    end

    -- Test: Node access after tree operations
    local tree6 = treesitter.parse("go", "package main\nfunc hello() {}")
    local root = tree6:root_node()
    local child = root:child(0)
    assert.not_nil(child, "child exists")

    -- Copy tree and access nodes
    local tree6_copy = tree6:copy()
    local copy_root = tree6_copy:root_node()
    assert.eq(copy_root:kind(), root:kind(), "copy has same structure")

    tree6:close()
    tree6_copy:close()

    -- Test: Query execution with many captures
    local big_code = [[
package main

func a() {}
func b() {}
func c() {}
func d() {}
func e() {}
]]
    local tree7 = treesitter.parse("go", big_code)
    local q = treesitter.query("go", "(function_declaration name: (identifier) @fn)")

    local captures = q:captures(tree7:root_node(), big_code)
    assert.eq(#captures, 5, "found 5 functions")

    -- Access all capture nodes
    for _, cap in ipairs(captures) do
        assert.not_nil(cap.node, "capture has node")
        assert.not_nil(cap.text, "capture has text")
        assert.ok(cap.node:start_byte() >= 0, "node has valid start")
        assert.ok(cap.node:end_byte() > cap.node:start_byte(), "node has valid range")
    end

    q:close()
    tree7:close()

    return true
end

return { main = main }
