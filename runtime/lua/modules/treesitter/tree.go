// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
)

const typeTree = "treesitter.Tree"

// pushTree pushes a Tree userdata to the stack
func pushTree(ctx context.Context, l *lua.LState, tree *treesitter.Tree, source string) {
	value.PushTypedUserData(l, NewTree(ctx, tree, source), typeTree)
}

// pushLanguageUD pushes a Language userdata to the stack
func pushLanguageUD(l *lua.LState, lang *treesitter.Language) {
	value.PushTypedUserData(l, &LanguageWrapper{lang: lang}, typeLanguage)
}

// TreeWrapper wraps a tree-sitter Tree for Lua integration
type TreeWrapper struct {
	tree          *treesitter.Tree
	cancelCleanup func()
	source        string
	closed        bool
}

// NewTree creates a new tree wrapper with proper resource store integration
func NewTree(ctx context.Context, tree *treesitter.Tree, source string) *TreeWrapper {
	wrapper := &TreeWrapper{
		tree:   tree,
		source: source,
	}

	// Register cleanup with resource store
	store := resource.GetStore(ctx)
	if store != nil {
		wrapper.cancelCleanup = store.AddCleanup(func() error {
			if wrapper.tree != nil && !wrapper.closed {
				wrapper.tree.Close()
				wrapper.tree = nil
				wrapper.closed = true
			}
			return nil
		})
	}

	return wrapper
}

// Close marks the tree as closed and cancels the cleanup
func (t *TreeWrapper) Close() {
	if !t.closed && t.cancelCleanup != nil {
		t.closed = true
		t.cancelCleanup()
		t.cancelCleanup = nil
	}
}

func treeRootNode(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is closed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	root := tree.tree.RootNode()
	if root == nil {
		l.Push(lua.LNil)
		return 1
	}

	pushNode(l, root, &tree.source)
	return 1
}

// RootNodeWithOffset implementation
func treeRootNodeWithOffset(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is closed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	offsetBytes := int(l.CheckNumber(2))
	offsetTable := l.CheckTable(3)

	offsetPoint := treesitter.Point{
		Row:    toUint(offsetTable.RawGetString("row")),
		Column: toUint(offsetTable.RawGetString("column")),
	}

	root := tree.tree.RootNodeWithOffset(offsetBytes, offsetPoint)
	if root == nil {
		l.Push(lua.LNil)
		return 1
	}

	pushNode(l, root, &tree.source)
	return 1
}

// Language implementation
func treeLanguage(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is closed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	lang := tree.tree.Language()
	if lang == nil {
		l.Push(lua.LNil)
		return 1
	}

	pushLanguageUD(l, lang)
	return 1
}

func treeCopy(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is closed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	copied := tree.tree.Clone()
	if copied == nil {
		l.Push(lua.LNil)
		return 1
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	pushTree(ctx, l, copied, tree.source)
	return 1
}

func treeWalk(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is closed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	cursor := tree.tree.Walk()
	if cursor == nil {
		l.Push(lua.LNil)
		return 1
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	cw := NewCursor(ctx, cursor, &tree.source)
	if cw == nil {
		l.Push(lua.LNil)
		return 1
	}

	pushCursor(l, cw)
	return 1
}

func (t *TreeWrapper) edit(edit *treesitter.InputEdit) error {
	if t.tree == nil {
		return fmt.Errorf("tree is nil")
	}
	t.tree.Edit(edit)
	return nil
}

// toNumber converts LValue to float64, handling both LNumber and LInteger
func toNumber(v lua.LValue) (float64, bool) {
	switch n := v.(type) {
	case lua.LNumber:
		return float64(n), true
	case lua.LInteger:
		return float64(n), true
	default:
		return 0, false
	}
}

// AddCleanup Lua binding
func treeEdit(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is closed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	editTable := l.CheckTable(2)

	// Helper for validation errors
	validationErr := func(msg string) int {
		err := lua.NewLuaError(l, msg).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	// Validate edit parameters
	startByteVal, ok := toNumber(editTable.RawGetString("start_byte"))
	if !ok {
		return validationErr("start_byte must be a number")
	}
	oldEndByteVal, ok := toNumber(editTable.RawGetString("old_end_byte"))
	if !ok {
		return validationErr("old_end_byte must be a number")
	}
	newEndByteVal, ok := toNumber(editTable.RawGetString("new_end_byte"))
	if !ok {
		return validationErr("new_end_byte must be a number")
	}

	// Basic validation of byte positions
	if startByteVal < 0 || oldEndByteVal < startByteVal || newEndByteVal < 0 {
		return validationErr("invalid byte position")
	}

	// Validate row/column positions
	startRowVal, ok := toNumber(editTable.RawGetString("start_row"))
	if !ok {
		return validationErr("start_row must be a number")
	}
	startColVal, ok := toNumber(editTable.RawGetString("start_column"))
	if !ok {
		return validationErr("start_column must be a number")
	}
	oldEndRowVal, ok := toNumber(editTable.RawGetString("old_end_row"))
	if !ok {
		return validationErr("old_end_row must be a number")
	}
	oldEndColVal, ok := toNumber(editTable.RawGetString("old_end_column"))
	if !ok {
		return validationErr("old_end_column must be a number")
	}
	newEndRowVal, ok := toNumber(editTable.RawGetString("new_end_row"))
	if !ok {
		return validationErr("new_end_row must be a number")
	}
	newEndColVal, ok := toNumber(editTable.RawGetString("new_end_column"))
	if !ok {
		return validationErr("new_end_column must be a number")
	}

	if startRowVal < 0 || startColVal < 0 || oldEndRowVal < startRowVal ||
		(oldEndRowVal == startRowVal && oldEndColVal < startColVal) ||
		newEndRowVal < 0 || newEndColVal < 0 {
		return validationErr("invalid point position")
	}

	startPoint := treesitter.Point{
		Row:    uint(startRowVal),
		Column: uint(startColVal),
	}
	oldEndPoint := treesitter.Point{
		Row:    uint(oldEndRowVal),
		Column: uint(oldEndColVal),
	}
	newEndPoint := treesitter.Point{
		Row:    uint(newEndRowVal),
		Column: uint(newEndColVal),
	}

	edit := &treesitter.InputEdit{
		StartByte:      uint(startByteVal),
		OldEndByte:     uint(oldEndByteVal),
		NewEndByte:     uint(newEndByteVal),
		StartPosition:  startPoint,
		OldEndPosition: oldEndPoint,
		NewEndPosition: newEndPoint,
	}

	if editErr := tree.edit(edit); editErr != nil {
		err := lua.WrapErrorWithLua(l, editErr, "edit failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// ChangedRanges implementation
func treeChangedRanges(l *lua.LState) int {
	tree := checkTree(l)
	if tree == nil {
		return 0
	}
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
	if tree == nil {
		return 0
	}
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
	if tree == nil {
		return 0
	}
	if tree.tree == nil {
		err := lua.NewLuaError(l, "tree is nil").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	// Spawn a pipe
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		err := lua.WrapErrorWithLua(l, pipeErr, "failed to create pipe").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	// Use a WaitGroup to ensure we read all data
	var wg sync.WaitGroup
	wg.Add(1)

	// Read from the pipe in a goroutine
	var buf bytes.Buffer
	var readErr error
	go func() { // todo: WTF
		defer wg.Done()
		_, readErr = buf.ReadFrom(r)
	}()

	// Write the DOT graph to the pipe
	tree.tree.PrintDotGraph(int(w.Fd()))
	_ = w.Close()

	// wait for reading to complete
	wg.Wait()

	if readErr != nil {
		err := lua.WrapErrorWithLua(l, readErr, "failed to read DOT graph").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	// Return the DOT graph as a string
	l.Push(lua.LString(buf.String()))
	return 1
}

func treeClose(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TreeWrapper); ok {
		v.Close()
	}
	return 0
}

// Helper functions

func checkTree(l *lua.LState) *TreeWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TreeWrapper); ok {
		if v.closed {
			l.ArgError(1, "tree already closed")
			return nil
		}

		return v
	}
	l.ArgError(1, "Tree expected")
	return nil
}
