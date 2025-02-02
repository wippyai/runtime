package btea

import (
	"github.com/charmbracelet/lipgloss/tree"
	lua "github.com/yuin/gopher-lua"
)

// Tree wraps tree.Model for Lua
type Tree struct {
	model *tree.Tree
}

// RegisterTree registers the tree component
func RegisterTree(l *lua.LState, mod *lua.LTable) {
	// Create and register the tree metatable
	mt := l.NewTypeMetatable("btea.Tree")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"root":             treeRoot,
		"child":            treeChild,
		"view":             treeView,
		"enumerator_style": treeEnumeratorStyle,
		"item_style":       treeItemStyle,
		"root_style":       treeRootStyle,
		"hide":             treeHide,
		"offset":           treeOffset,
	}))

	// Register constructor
	l.SetField(mod, "new_tree", l.NewFunction(newTree))

	// Register enumerator constants
	enumTbl := l.NewTable()
	l.SetField(enumTbl, "DEFAULT", l.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString("default"))
		return 1
	}))
	l.SetField(enumTbl, "ROUNDED", l.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString("rounded"))
		return 1
	}))
	l.SetField(mod, "tree_enumerator", enumTbl)
}

// Create a new tree instance
func newTree(l *lua.LState) int {
	// Create new tree
	t := tree.New()

	// Create userdata
	ud := l.NewUserData()
	ud.Value = &Tree{model: t}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Tree"))
	l.Push(ud)
	return 1
}

// Helper to get Tree from Lua userdata
func checkTree(l *lua.LState) *Tree {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Tree); ok {
		return v
	}
	l.ArgError(1, "tree expected")
	return nil
}

// Tree methods

func treeRoot(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}
	value := l.CheckString(2)
	t.model.Root(value)
	l.Push(l.NewUserData()) // Return self for chaining
	return 1
}

func treeChild(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	// Get variable number of children arguments
	children := make([]any, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		switch l.Get(i).Type() {
		case lua.LTString:
			children = append(children, l.ToString(i))
		case lua.LTUserData:
			if child, ok := l.ToUserData(i).Value.(*Tree); ok {
				children = append(children, child.model)
			}
		}
	}

	t.model.Child(children...)
	l.Push(l.ToUserData(1)) // Return self for chaining
	return 1
}

func treeView(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LString(t.model.String()))
	return 1
}

func treeEnumeratorStyle(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	if ud, ok := l.CheckUserData(2).Value.(*Style); ok {
		t.model.EnumeratorStyle(ud.style)
	}

	l.Push(l.ToUserData(1)) // Return self for chaining
	return 1
}

func treeItemStyle(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	if ud, ok := l.CheckUserData(2).Value.(*Style); ok {
		t.model.ItemStyle(ud.style)
	}

	l.Push(l.ToUserData(1)) // Return self for chaining
	return 1
}

func treeRootStyle(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	if ud, ok := l.CheckUserData(2).Value.(*Style); ok {
		t.model.RootStyle(ud.style)
	}

	l.Push(l.ToUserData(1)) // Return self for chaining
	return 1
}

func treeHide(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}
	hide := l.CheckBool(2)
	t.model.Hide(hide)
	l.Push(l.ToUserData(1)) // Return self for chaining
	return 1
}

func treeOffset(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}
	start := l.CheckInt(2)
	end := l.CheckInt(3)
	t.model.Offset(start, end)
	l.Push(l.ToUserData(1)) // Return self for chaining
	return 1
}
