package treesitter

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	"github.com/ponyruntime/pony/internal/closer"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

// TreeWrapper wraps a tree-sitter Tree for Lua integration
type TreeWrapper struct {
	tree   *treesitter.Tree
	source string // todo: change to byte
}

// Register the Tree type to Lua
func registerTree(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Tree")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), treeMethods))
}

var treeMethods = map[string]lua.LGFunction{
	"root_node":             treeRootNode,
	"root_node_with_offset": treeRootNodeWithOffset,
	"language":              treeLanguage,
	"copy":                  treeCopy,
	"walk":                  treeWalk,
	"edit":                  treeEdit,
	"close":                 treeClose,
	"changed_ranges":        treeChangedRanges,
	"included_ranges":       treeIncludedRanges,
	"dot_graph":             treePrintDotGraph,
}

// Tree methods implementation

func treeRootNode(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.RaiseError("tree is closed")
		return 0
	}

	root := tree.tree.RootNode()
	if root == nil {
		L.Push(lua.LNil)
		return 1
	}

	// Create and push new Node userdata
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: root, source: &tree.source}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

// RootNodeWithOffset implementation
func treeRootNodeWithOffset(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.RaiseError("tree is closed")
		return 0
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
	ud.Value = &NodeWrapper{node: root, source: &tree.source}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

// Language implementation
func treeLanguage(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.RaiseError("tree is closed")
		return 0
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
		L.RaiseError("tree is closed")
		return 0
	}

	copied := tree.tree.Clone()
	if copied == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = &TreeWrapper{tree: copied, source: tree.source}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Tree"))
	L.Push(ud)
	return 1
}

func treeWalk(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.RaiseError("tree is closed")
		return 0
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
	ud.Value = &CursorWrapper{cursor: cursor, source: &tree.source}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Cursor"))
	L.Push(ud)
	return 1
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
		L.RaiseError("tree is closed")
		return 0
	}

	editTable := L.CheckTable(2)

	// Validate edit parameters
	startByte := int32(editTable.RawGetString("start_byte").(lua.LNumber))
	oldEndByte := int32(editTable.RawGetString("old_end_byte").(lua.LNumber))
	newEndByte := int32(editTable.RawGetString("new_end_byte").(lua.LNumber))

	// Basic validation of byte positions
	if startByte < 0 || oldEndByte < startByte || newEndByte < 0 {
		L.Push(lua.LFalse)
		L.Push(lua.LString("invalid byte position"))
		return 2
	}

	// Validate row/column positions
	startRow := int32(editTable.RawGetString("start_row").(lua.LNumber))
	startCol := int32(editTable.RawGetString("start_column").(lua.LNumber))
	oldEndRow := int32(editTable.RawGetString("old_end_row").(lua.LNumber))
	oldEndCol := int32(editTable.RawGetString("old_end_column").(lua.LNumber))
	newEndRow := int32(editTable.RawGetString("new_end_row").(lua.LNumber))
	newEndCol := int32(editTable.RawGetString("new_end_column").(lua.LNumber))

	if startRow < 0 || startCol < 0 || oldEndRow < startRow ||
		(oldEndRow == startRow && oldEndCol < startCol) ||
		newEndRow < 0 || newEndCol < 0 {
		L.Push(lua.LFalse)
		L.Push(lua.LString("invalid point position"))
		return 2
	}

	startPoint := treesitter.Point{
		Row:    uint(startRow),
		Column: uint(startCol),
	}
	oldEndPoint := treesitter.Point{
		Row:    uint(oldEndRow),
		Column: uint(oldEndCol),
	}
	newEndPoint := treesitter.Point{
		Row:    uint(newEndRow),
		Column: uint(newEndCol),
	}

	edit := &treesitter.InputEdit{
		StartByte:      uint(startByte),
		OldEndByte:     uint(oldEndByte),
		NewEndByte:     uint(newEndByte),
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
		tree.tree = nil
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
