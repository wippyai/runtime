package treesitter

import (
	"bytes"
	"fmt"
	"github.com/ponyruntime/pony/internal/closer"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/yuin/gopher-lua"
	"os"
	"sync"
)

// TreeWrapper wraps a tree-sitter Tree for Lua integration
type TreeWrapper struct {
	tree *treesitter.Tree
}

// Register the Tree type to Lua
func registerTree(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Tree")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), treeMethods))
	L.SetField(mt, "__gc", L.NewFunction(treeGC))
}

var treeMethods = map[string]lua.LGFunction{
	"root_node":        treeRootNode,
	"root_node_offset": treeRootNodeWithOffset,
	"language":         treeLanguage,
	"copy":             treeCopy,
	"walk":             treeWalk,
	"edit":             treeEdit,
	"close":            treeClose,
	"changed_ranges":   treeChangedRanges,
	"included_ranges":  treeIncludedRanges,
	"print_dot_graph":  treePrintDotGraph,
}

// Tree methods implementation

func treeRootNode(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	root := tree.tree.RootNode()
	if root == nil {
		L.Push(lua.LNil)
		return 1
	}

	// Create and push new Node userdata
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: root}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

// RootNodeWithOffset implementation
func treeRootNodeWithOffset(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	// Get offset parameters
	offsetBytes := int(L.CheckNumber(2))
	offsetTable := L.CheckTable(3)

	offsetPoint := treesitter.Point{
		Row:    uint(offsetTable.RawGetString("row").(lua.LNumber)),
		Column: uint(offsetTable.RawGetString("column").(lua.LNumber)),
	}

	root := tree.tree.RootNodeWithOffset(offsetBytes, offsetPoint)
	if root == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: root}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

// Language implementation
func treeLanguage(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	lang := tree.tree.Language()
	if lang == nil {
		L.Push(lua.LNil)
		return 1
	}

	// Create and return Language userdata
	ud := L.NewUserData()
	ud.Value = &LanguageWrapper{lang: lang}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Language"))
	L.Push(ud)
	return 1
}

func treeCopy(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	copied := tree.tree.Clone()
	if copied == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = &TreeWrapper{tree: copied}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Tree"))
	L.Push(ud)
	return 1
}

func treeWalk(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	cursor := tree.tree.Walk()
	if cursor == nil {
		L.Push(lua.LNil)
		return 1
	}

	if L.Context() != nil {
		cleanup := closer.FromContext(L.Context())
		if cleanup != nil {
			cleanup.Add(func() error { cursor.Close(); return nil })
		}
	}

	ud := L.NewUserData()
	ud.Value = cursor
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Cursor"))
	L.Push(ud)
	return 1
}

func treeGC(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree != nil {
		tree.tree.Close()
	}
	return 0
}

func (t *TreeWrapper) Edit(edit *treesitter.InputEdit) error {
	if t.tree == nil {
		return fmt.Errorf("tree is nil")
	}
	t.tree.Edit(edit)
	return nil
}

// Add Lua binding
func treeEdit(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.ArgError(1, "tree is closed")
		return 0
	}

	editTable := L.CheckTable(2)

	startPoint := treesitter.Point{
		Row:    uint(editTable.RawGetString("start_row").(lua.LNumber)),
		Column: uint(editTable.RawGetString("start_column").(lua.LNumber)),
	}

	oldEndPoint := treesitter.Point{
		Row:    uint(editTable.RawGetString("old_end_row").(lua.LNumber)),
		Column: uint(editTable.RawGetString("old_end_column").(lua.LNumber)),
	}

	newEndPoint := treesitter.Point{
		Row:    uint(editTable.RawGetString("new_end_row").(lua.LNumber)),
		Column: uint(editTable.RawGetString("new_end_column").(lua.LNumber)),
	}

	edit := &treesitter.InputEdit{
		StartByte:      uint(editTable.RawGetString("start_byte").(lua.LNumber)),
		OldEndByte:     uint(editTable.RawGetString("old_end_byte").(lua.LNumber)),
		NewEndByte:     uint(editTable.RawGetString("new_end_byte").(lua.LNumber)),
		StartPosition:  startPoint,
		OldEndPosition: oldEndPoint,
		NewEndPosition: newEndPoint,
	}

	if err := tree.Edit(edit); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// ChangedRanges implementation
func treeChangedRanges(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	otherUD := L.CheckUserData(2)
	otherTree, ok := otherUD.Value.(*TreeWrapper)
	if !ok {
		L.ArgError(2, "Tree expected")
		return 0
	}

	ranges := tree.tree.ChangedRanges(otherTree.tree)

	// Create Lua table to hold the ranges
	rangesTable := L.NewTable()
	for i, r := range ranges {
		rangeTable := L.NewTable()

		startPoint := L.NewTable()
		startPoint.RawSetString("row", lua.LNumber(r.StartPoint.Row))
		startPoint.RawSetString("column", lua.LNumber(r.StartPoint.Column))

		endPoint := L.NewTable()
		endPoint.RawSetString("row", lua.LNumber(r.EndPoint.Row))
		endPoint.RawSetString("column", lua.LNumber(r.EndPoint.Column))

		rangeTable.RawSetString("start_point", startPoint)
		rangeTable.RawSetString("end_point", endPoint)
		rangeTable.RawSetString("start_byte", lua.LNumber(r.StartByte))
		rangeTable.RawSetString("end_byte", lua.LNumber(r.EndByte))

		rangesTable.RawSetInt(i+1, rangeTable)
	}

	L.Push(rangesTable)
	return 1
}

// IncludedRanges implementation
func treeIncludedRanges(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	ranges := tree.tree.IncludedRanges()

	// Create Lua table to hold the ranges
	rangesTable := L.NewTable()
	for i, r := range ranges {
		rangeTable := L.NewTable()

		startPoint := L.NewTable()
		startPoint.RawSetString("row", lua.LNumber(r.StartPoint.Row))
		startPoint.RawSetString("column", lua.LNumber(r.StartPoint.Column))

		endPoint := L.NewTable()
		endPoint.RawSetString("row", lua.LNumber(r.EndPoint.Row))
		endPoint.RawSetString("column", lua.LNumber(r.EndPoint.Column))

		rangeTable.RawSetString("start_point", startPoint)
		rangeTable.RawSetString("end_point", endPoint)
		rangeTable.RawSetString("start_byte", lua.LNumber(r.StartByte))
		rangeTable.RawSetString("end_byte", lua.LNumber(r.EndByte))

		rangesTable.RawSetInt(i+1, rangeTable)
	}

	L.Push(rangesTable)
	return 1
}

// Print a graph of the tree and return it as a string
func treePrintDotGraph(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("tree is nil"))
		return 2
	}

	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to create pipe: " + err.Error()))
		return 2
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	// Use a WaitGroup to ensure we read all data
	var wg sync.WaitGroup
	wg.Add(1)

	// Read from the pipe in a goroutine
	var buf bytes.Buffer
	var readErr error
	go func() {
		defer wg.Done()
		_, readErr = buf.ReadFrom(r)
	}()

	// Write the DOT graph to the pipe
	tree.tree.PrintDotGraph(int(w.Fd()))
	_ = w.Close()

	// Wait for reading to complete
	wg.Wait()

	if readErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to read DOT graph: " + readErr.Error()))
		return 2
	}

	// Return the DOT graph as a string
	L.Push(lua.LString(buf.String()))
	return 1
}

func treeClose(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree != nil {
		tree.tree.Close()
	}
	return 0
}

// Helper functions

func checkTree(L *lua.LState) *TreeWrapper {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*TreeWrapper); ok {
		return v
	}
	L.ArgError(1, "Tree expected")
	return nil
}
