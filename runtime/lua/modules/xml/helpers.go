// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/system/xml"
)

// elementArg returns the Element table at index n or raises an argument error.
func elementArg(l *lua.LState, n int) *lua.LTable {
	t, ok := l.Get(n).(*lua.LTable)
	if !ok {
		raise(l, argument(l, "element must be a table"))
	}
	return t
}

func stringArg(l *lua.LState, n int, what string) string {
	s, ok := l.Get(n).(lua.LString)
	if !ok {
		raise(l, argument(l, what+" must be a string"))
	}
	return string(s)
}

// optionalString returns the string at index n, or absent when it is nil.
func optionalString(l *lua.LState, n int, what string) (string, bool) {
	switch v := l.Get(n).(type) {
	case *lua.LNilType:
		return "", false
	case lua.LString:
		return string(v), true
	default:
		raise(l, argument(l, what+" must be a string"))
		return "", false
	}
}

func nameOf(t *lua.LTable) (string, string) {
	name, _ := t.RawGetString("name").(lua.LString)
	ns, _ := t.RawGetString("ns").(lua.LString)
	return string(name), string(ns)
}

// childElements calls visit for each child Element table in document order.
func childElements(t *lua.LTable, visit func(*lua.LTable) bool) {
	c, ok := t.RawGetString("children").(*lua.LTable)
	if !ok {
		return
	}
	for _, v := range c.Array {
		if e, ok := v.(*lua.LTable); ok && !visit(e) {
			return
		}
	}
}

// Helpers that walk descendants spend a budget equal to the source a tree decoded under
// max_node_bytes could hold: 4 units per element visited and 1 per text byte. Depth is
// bounded by max_depth, so a cyclic table ends in a limit error.
const (
	elementCost         = 4
	defaultHelperBudget = 8 << 20
	defaultHelperDepth  = 256
)

type budget struct {
	exceeded string
	units    int
	maxDepth int
}

func (b *budget) spend(n int) bool {
	b.units -= n
	if b.units < 0 {
		b.exceeded = "max_node_bytes"
		return false
	}
	return true
}

func (b *budget) enter(depth int) bool {
	if depth > b.maxDepth {
		b.exceeded = "max_depth"
		return false
	}
	return b.spend(elementCost)
}

func (b *budget) err(l *lua.LState) *lua.Error {
	return lua.NewLuaError(l, b.exceeded+" exceeded").WithKind(lua.Invalid).WithRetryable(false).
		WithDetails(map[string]any{"code": "limit", "limit": b.exceeded})
}

// helperOptions parses {max_node_bytes, max_depth} and, for find, ns. Bad options raise.
func helperOptions(l *lua.LState, n int, withNS bool) (*budget, map[string]string) {
	b := &budget{units: defaultHelperBudget, maxDepth: defaultHelperDepth}
	var ns map[string]string
	t, err := optionTable(l, n)
	if err == nil {
		err = eachOption(l, t, func(key string, v lua.LValue) *lua.Error {
			var failure *lua.Error
			var limit int64
			switch {
			case key == "max_node_bytes":
				limit, failure = integerOption(l, key, v, 1)
				b.units = int(limit)
			case key == "max_depth":
				limit, failure = integerOption(l, key, v, 1)
				b.maxDepth = int(limit)
			case key == "ns" && withNS:
				ns, failure = stringMapOption(l, key, v)
			default:
				return argument(l, "unknown option "+key)
			}
			return failure
		})
	}
	if err != nil {
		raise(l, err)
	}
	return b, ns
}

func appendText(sb *strings.Builder, t *lua.LTable, depth int, b *budget) bool {
	if !b.enter(depth) {
		return false
	}
	if s, ok := t.RawGetString("text").(lua.LString); ok {
		if !b.spend(len(s)) {
			return false
		}
		sb.WriteString(string(s))
	}
	c, ok := t.RawGetString("children").(*lua.LTable)
	if !ok {
		return true
	}
	for _, v := range c.Array {
		switch x := v.(type) {
		case lua.LString:
			if !b.spend(len(x)) {
				return false
			}
			sb.WriteString(string(x))
		case *lua.LTable:
			if !appendText(sb, x, depth+1, b) {
				return false
			}
		}
	}
	return true
}

func textFunc(l *lua.LState) int {
	t := elementArg(l, 1)
	b, _ := helperOptions(l, 2, false)
	var sb strings.Builder
	if !appendText(&sb, t, 1, b) {
		return raise(l, b.err(l))
	}
	l.Push(lua.LString(sb.String()))
	return 1
}

func attrFunc(l *lua.LState) int {
	t := elementArg(l, 1)
	name := stringArg(l, 2, "name")
	ns, _ := optionalString(l, 3, "ns")
	if attrs, ok := t.RawGetString("attrs").(*lua.LTable); ok {
		if v, ok := attrs.RawGetString(xml.AttrKey(ns, name)).(lua.LString); ok {
			l.Push(v)
			return 1
		}
	}
	l.Push(lua.LNil)
	return 1
}

func matches(e *lua.LTable, name string, hasName bool, ns string, hasNS bool) bool {
	n, u := nameOf(e)
	return (!hasName || n == name) && (!hasNS || u == ns)
}

func childFunc(l *lua.LState) int {
	t := elementArg(l, 1)
	name := stringArg(l, 2, "name")
	ns, hasNS := optionalString(l, 3, "ns")
	found := lua.LNil
	childElements(t, func(e *lua.LTable) bool {
		if matches(e, name, true, ns, hasNS) {
			found = e
			return false
		}
		return true
	})
	l.Push(found)
	return 1
}

func childrenFunc(l *lua.LState) int {
	t := elementArg(l, 1)
	name, hasName := optionalString(l, 2, "name")
	ns, hasNS := optionalString(l, 3, "ns")
	out := lua.CreateTable(0, 0)
	childElements(t, func(e *lua.LTable) bool {
		if matches(e, name, hasName, ns, hasNS) {
			out.Append(e)
		}
		return true
	})
	l.Push(out)
	return 1
}

func findFunc(l *lua.LState) int {
	t := elementArg(l, 1)
	path := stringArg(l, 2, "path")
	b, bindings := helperOptions(l, 3, true)
	p, err := xml.CompilePath(path, bindings)
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	out := lua.CreateTable(0, 0)
	last := p.Len() - 1
	var walk func(*lua.LTable, int) bool
	walk = func(e *lua.LTable, i int) bool {
		within := true
		childElements(e, func(c *lua.LTable) bool {
			if !b.enter(i + 2) {
				within = false
				return false
			}
			name, ns := nameOf(c)
			if p.Match(i, name, ns) {
				if i == last {
					out.Append(c)
				} else if !walk(c, i+1) {
					within = false
					return false
				}
			}
			return true
		})
		return within
	}
	if !b.enter(1) || !walk(t, 0) {
		return fail(l, lua.LNil, b.err(l))
	}
	l.Push(out)
	l.Push(lua.LNil)
	return 2
}

func resolveFunc(l *lua.LState) int {
	t, ok := l.Get(1).(*lua.LTable)
	if !ok {
		l.Push(lua.LNil)
		return fail(l, lua.LNil, argument(l, "element must be a table")) + 1
	}
	qname, ok := l.Get(2).(lua.LString)
	if !ok {
		l.Push(lua.LNil)
		return fail(l, lua.LNil, argument(l, "qname must be a string")) + 1
	}
	xmlns, _ := t.RawGetString("xmlns").(*lua.LTable)
	local, ns, err := xml.ResolveQName(string(qname), func(prefix string) (string, bool) {
		if xmlns == nil {
			return "", false
		}
		uri, ok := xmlns.RawGetString(prefix).(lua.LString)
		return string(uri), ok
	})
	if err != nil {
		l.Push(lua.LNil)
		return fail(l, lua.LNil, luaError(l, err)) + 1
	}
	l.Push(lua.LString(local))
	if ns == "" {
		l.Push(lua.LNil)
	} else {
		l.Push(lua.LString(ns))
	}
	l.Push(lua.LNil)
	return 3
}
