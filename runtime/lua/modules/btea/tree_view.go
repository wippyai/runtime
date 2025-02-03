package btea

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
	lua "github.com/yuin/gopher-lua"
)

// todo: finish it

// LuaChildren wraps tree.Children for Lua integration
type LuaChildren struct {
	children tree.Children
}

// Implement lua.LValue interface for LuaChildren
func (lc *LuaChildren) String() string                         { return "tree.Children" }
func (lc *LuaChildren) Type() lua.LValueType                   { return lua.LTUserData }
func (lc *LuaChildren) AssertFloat64() (float64, bool)         { return 0, false }
func (lc *LuaChildren) AssertString() (string, bool)           { return "", false }
func (lc *LuaChildren) AssertFunction() (*lua.LFunction, bool) { return nil, false }
func (lc *LuaChildren) Peek() lua.LValue                       { return lc }

// LuaLeaf wraps tree.Leaf for Lua integration
type LuaLeaf struct {
	leaf *tree.Leaf
}

// Implement lua.LValue interface for LuaLeaf
func (ll *LuaLeaf) String() string                         { return ll.leaf.Value() }
func (ll *LuaLeaf) Type() lua.LValueType                   { return lua.LTUserData }
func (ll *LuaLeaf) AssertFloat64() (float64, bool)         { return 0, false }
func (ll *LuaLeaf) AssertString() (string, bool)           { return ll.leaf.Value(), true }
func (ll *LuaLeaf) AssertFunction() (*lua.LFunction, bool) { return nil, false }
func (ll *LuaLeaf) Peek() lua.LValue                       { return ll }

// Create Lua wrappers for Children
func wrapChildren(l *lua.LState, children tree.Children) lua.LValue {
	ud := l.NewUserData()
	ud.Value = &LuaChildren{children: children}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Children"))
	return ud
}

// Create Lua wrappers for Leaf
func wrapLeaf(l *lua.LState, leaf *tree.Leaf) lua.LValue {
	ud := l.NewUserData()
	ud.Value = &LuaLeaf{leaf: leaf}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Leaf"))
	return ud
}

// Get the underlying Children from a Lua value
func unwrapChildren(v lua.LValue) (tree.Children, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if lc, ok := ud.Value.(*LuaChildren); ok {
			return lc.children, true
		}
	}
	return nil, false
}

// Get the underlying Leaf from a Lua value
func unwrapLeaf(v lua.LValue) (*tree.Leaf, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if ll, ok := ud.Value.(*LuaLeaf); ok {
			return ll.leaf, true
		}
	}
	return nil, false
}

// Register children metatable
func registerChildrenMetatable(l *lua.LState) {
	mt := l.NewTypeMetatable("btea.Children")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"length": childrenLength,
		"at":     childrenAt,
	}))
}

// Children methods
func childrenLength(l *lua.LState) int {
	if ud := l.CheckUserData(1); ud != nil {
		if lc, ok := ud.Value.(*LuaChildren); ok {
			l.Push(lua.LNumber(lc.children.Length()))
			return 1
		}
	}
	l.ArgError(1, "Children expected")
	return 0
}

func childrenAt(l *lua.LState) int {
	if ud := l.CheckUserData(1); ud != nil {
		if lc, ok := ud.Value.(*LuaChildren); ok {
			index := l.CheckInt(2)
			node := lc.children.At(index)
			if node == nil {
				l.Push(lua.LNil)
				return 1
			}

			// Handle different node types
			switch n := node.(type) {
			case *tree.Tree:
				ud := l.NewUserData()
				ud.Value = &TreeView{model: n}
				l.SetMetatable(ud, l.GetTypeMetatable("btea.TreeView"))
				l.Push(ud)
			case *tree.Leaf:
				l.Push(wrapLeaf(l, n))
			default:
				l.Push(lua.LString(n.Value()))
			}
			return 1
		}
	}
	l.ArgError(1, "Children expected")
	return 0
}

// TreeView wraps tree.Model for Lua
type TreeView struct {
	model *tree.Tree
	// Store Lua callbacks for styling and enumeration
	itemStyleFn  *lua.LFunction
	enumeratorFn *lua.LFunction
	indenterFn   *lua.LFunction
	filterFn     *lua.LFunction
	luaState     *lua.LState
}

// RegisterTree registers the tree component
func RegisterTree(l *lua.LState, mod *lua.LTable) {
	registerChildrenMetatable(l)

	// Create and register the tree metatable
	mt := l.NewTypeMetatable("btea.TreeView")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core methods
		"view":   treeView,
		"root":   treeRoot,
		"child":  treeChild,
		"filter": treeFilter,
		"hide":   treeHide,
		"offset": treeOffset,

		// Styling methods
		"root_style":            treeRootStyle,
		"item_style":            treeItemStyle,
		"item_style_func":       treeItemStyleFunc,
		"enumerator_style":      treeEnumeratorStyle,
		"enumerator_style_func": treeEnumeratorStyleFunc,
		"enumerator":            treeEnumerator,
		"indenter":              treeIndenter,
	}))

	// Register constructor
	l.SetField(mod, "new_tree", l.NewFunction(newTree))

	// Register built-in enumerators
	enumeratorsTbl := l.NewTable()
	l.SetField(enumeratorsTbl, "DEFAULT", luaEnumeratorFromGo(l, tree.DefaultEnumerator))
	l.SetField(enumeratorsTbl, "ROUNDED", luaEnumeratorFromGo(l, tree.RoundedEnumerator))
	l.SetField(mod, "enumerators", enumeratorsTbl)

	// Register built-in indenters
	indentersTbl := l.NewTable()
	l.SetField(indentersTbl, "DEFAULT", luaIndenterFromGo(l, tree.DefaultIndenter))
	l.SetField(mod, "indenters", indentersTbl)
}

func treeView(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LString(t.model.String()))
	return 1
}

func treeRoot(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	value := l.CheckAny(2)
	switch v := value.(type) {
	case *lua.LUserData:
		if tree, ok := v.Value.(*TreeView); ok {
			t.model.Root(tree.model)
		}
	case lua.LString:
		t.model.Root(string(v))
	default:
		t.model.Root(lua.LVAsString(value))
	}

	l.Push(l.CheckUserData(1))
	return 1
}

func treeChild(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	top := l.GetTop()
	children := make([]any, 0, top-1)

	for i := 2; i <= top; i++ {
		value := l.Get(i)
		switch v := value.(type) {
		case *lua.LUserData:
			if tm, ok := v.Value.(*TreeView); ok {
				children = append(children, tm.model)
			} else if luaLeaf, ok := v.Value.(*LuaLeaf); ok {
				children = append(children, luaLeaf.leaf)
			}
		case *lua.LTable:
			v.ForEach(func(_, item lua.LValue) {
				children = append(children, lua.LVAsString(item))
			})
		case lua.LString:
			children = append(children, string(v))
		default:
			children = append(children, lua.LVAsString(value))
		}
	}

	t.model.Child(children...)
	l.Push(l.CheckUserData(1))
	return 1
}

func treeFilter(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	fn := l.CheckFunction(2)
	t.filterFn = fn

	filter := tree.NewFilter(t.model.Children())
	filter.Filter(t.makeFilter(fn))
	t.model = tree.New().Root(t.model.Value()).Child(filter)

	l.Push(l.CheckUserData(1))
	return 1
}

func treeHide(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	hide := l.CheckBool(2)
	t.model.Hide(hide)
	l.Push(l.CheckUserData(1))
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
	l.Push(l.CheckUserData(1))
	return 1
}

// Styling methods

func treeRootStyle(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	style := checkStyle(l)
	if style == nil {
		return 0
	}

	t.model.RootStyle(style.Style)
	l.Push(l.CheckUserData(1))
	return 1
}

func treeItemStyle(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	style := checkStyle(l)
	if style == nil {
		return 0
	}

	t.model.ItemStyle(style.Style)
	l.Push(l.CheckUserData(1))
	return 1
}

func treeItemStyleFunc(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	fn := l.CheckFunction(2)
	t.itemStyleFn = fn
	t.model.ItemStyleFunc(t.makeStyleFunc(fn))
	l.Push(l.CheckUserData(1))
	return 1
}

func treeEnumeratorStyle(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	style := checkStyle(l)
	if style == nil {
		return 0
	}

	t.model.EnumeratorStyle(style.Style)
	l.Push(l.CheckUserData(1))
	return 1
}

func treeEnumeratorStyleFunc(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	fn := l.CheckFunction(2)
	t.itemStyleFn = fn
	t.model.EnumeratorStyleFunc(t.makeStyleFunc(fn))
	l.Push(l.CheckUserData(1))
	return 1
}

func treeEnumerator(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	fn := l.CheckFunction(2)
	t.enumeratorFn = fn
	t.model.Enumerator(t.makeEnumerator(fn))
	l.Push(l.CheckUserData(1))
	return 1
}

func treeIndenter(l *lua.LState) int {
	t := checkTree(l)
	if t == nil {
		return 0
	}

	fn := l.CheckFunction(2)
	t.indenterFn = fn
	t.model.Indenter(t.makeIndenter(fn))
	l.Push(l.CheckUserData(1))
	return 1
}

func newTree(l *lua.LState) int {
	t := &TreeView{
		model:    tree.New(),
		luaState: l,
	}

	// If options table is provided, process it
	if l.GetTop() > 0 {
		opts := l.CheckTable(1)
		opts.ForEach(func(k, v lua.LValue) {
			switch k.String() {
			case "root":
				if str, ok := v.(lua.LString); ok {
					t.model.Root(string(str))
				}
			case "root_style":
				if ud, ok := v.(*lua.LUserData); ok {
					if style, ok := ud.Value.(*Style); ok {
						t.model.RootStyle(style.Style)
					}
				}
			case "item_style":
				if ud, ok := v.(*lua.LUserData); ok {
					if style, ok := ud.Value.(*Style); ok {
						t.model.ItemStyle(style.Style)
					}
				}
			case "enumerator_style":
				if ud, ok := v.(*lua.LUserData); ok {
					if style, ok := ud.Value.(*Style); ok {
						t.model.EnumeratorStyle(style.Style)
					}
				}
			case "item_style_func":
				if fn, ok := v.(*lua.LFunction); ok {
					t.itemStyleFn = fn
					t.model.ItemStyleFunc(t.makeStyleFunc(fn))
				}
			case "enumerator":
				if fn, ok := v.(*lua.LFunction); ok {
					t.enumeratorFn = fn
					t.model.Enumerator(t.makeEnumerator(fn))
				}
			case "indenter":
				if fn, ok := v.(*lua.LFunction); ok {
					t.indenterFn = fn
					t.model.Indenter(t.makeIndenter(fn))
				}
			case "filter":
				if fn, ok := v.(*lua.LFunction); ok {
					t.filterFn = fn
					filter := tree.NewFilter(t.model.Children())
					filter.Filter(t.makeFilter(fn))
					t.model = tree.New().Root(t.model.Value()).Child(filter)
				}
			}
		})
	}

	ud := l.NewUserData()
	ud.Value = t
	l.SetMetatable(ud, l.GetTypeMetatable("btea.TreeView"))
	l.Push(ud)
	return 1
}

func checkTree(l *lua.LState) *TreeView {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TreeView); ok {
		return v
	}
	l.ArgError(1, "tree expected")
	return nil
}

// Helper functions for converting Go callbacks to/from Lua

func (t *TreeView) makeStyleFunc(fn *lua.LFunction) func(tree.Children, int) lipgloss.Style {
	return func(children tree.Children, index int) lipgloss.Style {
		err := t.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, wrapChildren(t.luaState, children), lua.LNumber(index))

		if err != nil {
			return lipgloss.NewStyle()
		}

		ret := t.luaState.Get(-1)
		t.luaState.Pop(1)

		if ud, ok := ret.(*lua.LUserData); ok {
			if style, ok := ud.Value.(*Style); ok {
				return style.Style
			}
		}

		return lipgloss.NewStyle()
	}
}

func (t *TreeView) makeEnumerator(fn *lua.LFunction) tree.Enumerator {
	return func(children tree.Children, index int) string {
		err := t.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, wrapChildren(t.luaState, children), lua.LNumber(index))

		if err != nil {
			return "├──"
		}

		ret := t.luaState.Get(-1)
		t.luaState.Pop(1)

		if str, ok := ret.(lua.LString); ok {
			return string(str)
		}

		return "├──"
	}
}

func (t *TreeView) makeIndenter(fn *lua.LFunction) tree.Indenter {
	return func(children tree.Children, index int) string {
		err := t.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, wrapChildren(t.luaState, children), lua.LNumber(index))

		if err != nil {
			return "   "
		}

		ret := t.luaState.Get(-1)
		t.luaState.Pop(1)

		if str, ok := ret.(lua.LString); ok {
			return string(str)
		}

		return "   "
	}
}

func (t *TreeView) makeFilter(fn *lua.LFunction) func(int) bool {
	return func(index int) bool {
		err := t.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, lua.LNumber(index))

		if err != nil {
			return true
		}

		ret := t.luaState.Get(-1)
		t.luaState.Pop(1)

		if b, ok := ret.(lua.LBool); ok {
			return bool(b)
		}

		return true
	}
}

func luaEnumeratorFromGo(l *lua.LState, e tree.Enumerator) *lua.LFunction {
	return l.NewFunction(func(l *lua.LState) int {
		children := l.CheckUserData(1)
		index := l.CheckInt(2)
		if c, ok := children.Value.(tree.Children); ok {
			l.Push(lua.LString(e(c, index)))
			return 1
		}
		l.Push(lua.LString("├──"))
		return 1
	})
}

func luaIndenterFromGo(l *lua.LState, i tree.Indenter) *lua.LFunction {
	return l.NewFunction(func(l *lua.LState) int {
		children := l.CheckUserData(1)
		index := l.CheckInt(2)
		if c, ok := children.Value.(tree.Children); ok {
			l.Push(lua.LString(i(c, index)))
			return 1
		}
		l.Push(lua.LString("   "))
		return 1
	})
}
