package treesitter

import (
	"bytes"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"os"
	"sync"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

// TreeWrapper wraps a tree-sitter Tree for Lua integration
type TreeWrapper struct {
	tree   *treesitter.Tree
	once   sync.Once
	source string // todo: change to byte
}

func (t *TreeWrapper) Close() {
	t.once.Do(func() {
		if t.tree != nil {
			//t.tree.Close()
			//t.tree = nil
		}
	})
}

// Register the Tree type to Lua
func registerTree(l *lua.LState) {
	methods := map[string]lua.LGFunction{
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
	value.RegisterMethods(l, "treesitter.Tree", methods)
}

func treeRootNode(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.RaiseError("tree is closed")
		return 0
	}

	root := tree.tree.RootNode()
	if root == nil {
		l.Push(lua.LNil)
		return 1
	}

	// Spawn and push new Node userdata
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: root, source: &tree.source}
	ud.Metatable = l.NewTypeMetatable("treesitter.Node")

	l.Push(ud)
	return 1
}

// RootNodeWithOffset implementation
func treeRootNodeWithOffset(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.RaiseError("tree is closed")
		return 0
	}

	// Spawn offset parameters
	offsetBytes := int(l.CheckNumber(2))
	offsetTable := l.CheckTable(3)

	offsetPoint := treesitter.Point{
		Row:    uint(offsetTable.RawGetString("row").(lua.LNumber)),
		Column: uint(offsetTable.RawGetString("column").(lua.LNumber)),
	}

	root := tree.tree.RootNodeWithOffset(offsetBytes, offsetPoint)
	if root == nil {
		l.Push(lua.LNil)
		return 1
	}

	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: root, source: &tree.source}
	ud.Metatable = l.NewTypeMetatable("treesitter.Node")

	l.Push(ud)
	return 1
}

// Language implementation
func treeLanguage(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.RaiseError("tree is closed")
		return 0
	}

	lang := tree.tree.Language()
	if lang == nil {
		l.Push(lua.LNil)
		return 1
	}

	// Spawn and return Language userdata
	ud := l.NewUserData()
	ud.Value = &LanguageWrapper{lang: lang}
	ud.Metatable = l.NewTypeMetatable("treesitter.Language")

	l.Push(ud)
	return 1
}

func treeCopy(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.RaiseError("tree is closed")
		return 0
	}

	copied := tree.tree.Clone()
	if copied == nil {
		l.Push(lua.LNil)
		return 1
	}

	ud := l.NewUserData()
	ud.Value = &TreeWrapper{tree: copied, source: tree.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Tree")

	l.Push(ud)
	return 1
}

func treeWalk(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.RaiseError("tree is closed")
		return 0
	}

	cursor := tree.tree.Walk()
	if cursor == nil {
		l.Push(lua.LNil)
		return 1
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work is not found")
		return 1
	}

	cw := &CursorWrapper{cursor: cursor, source: &tree.source}

	ud := l.NewUserData()
	ud.Value = cw
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Cursor")

	uw.AddCleanup(func() error {
		cw.once.Do(func() {
			if cw.cursor != nil {
				//cw.cursor.Close()
				//cw.cursor = nil
			}
		})
		return nil
	})

	l.Push(ud)
	return 1
}

func (t *TreeWrapper) edit(edit *treesitter.InputEdit) error {
	if t.tree == nil {
		return fmt.Errorf("tree is nil")
	}
	t.tree.Edit(edit)
	return nil
}

// AddCleanup Lua binding
func treeEdit(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.RaiseError("tree is closed")
		return 0
	}

	editTable := l.CheckTable(2)

	// Validate edit parameters
	startByte := editTable.RawGetString("start_byte").(lua.LNumber)
	oldEndByte := editTable.RawGetString("old_end_byte").(lua.LNumber)
	newEndByte := editTable.RawGetString("new_end_byte").(lua.LNumber)

	// Basic validation of byte positions
	if startByte < 0 || oldEndByte < startByte || newEndByte < 0 {
		l.Push(lua.LFalse)
		l.Push(lua.LString("invalid byte position"))
		return 2
	}

	// Validate row/column positions
	// TODO: potentially dangerous type assertion, todo: fix it!
	startRow := editTable.RawGetString("start_row").(lua.LNumber)
	startCol := editTable.RawGetString("start_column").(lua.LNumber)
	oldEndRow := editTable.RawGetString("old_end_row").(lua.LNumber)
	oldEndCol := editTable.RawGetString("old_end_column").(lua.LNumber)
	newEndRow := editTable.RawGetString("new_end_row").(lua.LNumber)
	newEndCol := editTable.RawGetString("new_end_column").(lua.LNumber)

	if startRow < 0 || startCol < 0 || oldEndRow < startRow ||
		(oldEndRow == startRow && oldEndCol < startCol) ||
		newEndRow < 0 || newEndCol < 0 {
		l.Push(lua.LFalse)
		l.Push(lua.LString("invalid point position"))
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

	if err := tree.edit(edit); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// ChangedRanges implementation
func treeChangedRanges(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.Push(lua.LNil)
		return 1
	}

	otherUD := l.CheckUserData(2)
	otherTree, ok := otherUD.Value.(*TreeWrapper)
	if !ok {
		l.ArgError(2, "Tree expected")
		return 0
	}

	ranges := tree.tree.ChangedRanges(otherTree.tree)

	// Spawn Lua table to hold the ranges
	rangesTable := l.CreateTable(len(ranges), 0)
	for i, r := range ranges {

		startPoint := l.CreateTable(0, 2)
		startPoint.RawSetString("row", lua.LNumber(r.StartPoint.Row))
		startPoint.RawSetString("column", lua.LNumber(r.StartPoint.Column))

		endPoint := l.CreateTable(0, 2)
		endPoint.RawSetString("row", lua.LNumber(r.EndPoint.Row))
		endPoint.RawSetString("column", lua.LNumber(r.EndPoint.Column))

		rangeTable := l.CreateTable(0, 4)
		rangeTable.RawSetString("start_point", startPoint)
		rangeTable.RawSetString("end_point", endPoint)
		rangeTable.RawSetString("start_byte", lua.LNumber(r.StartByte))
		rangeTable.RawSetString("end_byte", lua.LNumber(r.EndByte))

		rangesTable.RawSetInt(i+1, rangeTable)
	}

	l.Push(rangesTable)
	return 1
}

// IncludedRanges implementation
func treeIncludedRanges(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.Push(lua.LNil)
		return 1
	}

	ranges := tree.tree.IncludedRanges()

	// Spawn Lua table to hold the ranges
	rangesTable := l.CreateTable(len(ranges), 0)
	for i, r := range ranges {
		startPoint := l.CreateTable(0, 2)
		startPoint.RawSetString("row", lua.LNumber(r.StartPoint.Row))
		startPoint.RawSetString("column", lua.LNumber(r.StartPoint.Column))

		endPoint := l.CreateTable(0, 2)
		endPoint.RawSetString("row", lua.LNumber(r.EndPoint.Row))
		endPoint.RawSetString("column", lua.LNumber(r.EndPoint.Column))

		rangeTable := l.CreateTable(0, 4)
		rangeTable.RawSetString("start_point", startPoint)
		rangeTable.RawSetString("end_point", endPoint)
		rangeTable.RawSetString("start_byte", lua.LNumber(r.StartByte))
		rangeTable.RawSetString("end_byte", lua.LNumber(r.EndByte))

		rangesTable.RawSetInt(i+1, rangeTable)
	}

	l.Push(rangesTable)
	return 1
}

// Print a graph of the tree and return it as a string
func treePrintDotGraph(l *lua.LState) int {
	tree := checkTree(l)
	if tree.tree == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("tree is nil"))
		return 2
	}

	// Spawn a pipe
	r, w, err := os.Pipe()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to create pipe: " + err.Error()))
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

	// wait for reading to complete
	wg.Wait()

	if readErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to read DOT graph: " + readErr.Error()))
		return 2
	}

	// Return the DOT graph as a string
	l.Push(lua.LString(buf.String()))
	return 1
}

func treeClose(l *lua.LState) int {
	tree := checkTree(l)
	tree.Close()
	return 0
}

// Helper functions

func checkTree(l *lua.LState) *TreeWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TreeWrapper); ok {
		return v
	}
	l.ArgError(1, "Tree expected")
	return nil
}
