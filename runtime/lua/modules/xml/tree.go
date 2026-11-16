// SPDX-License-Identifier: MPL-2.0

package xml

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/system/xml"
)

// Interning matches the core decoder: names up to 1 KiB are shared, short values are
// shared, and the strings retained by one interner are bounded in total.
const (
	maxInterned      = 8192
	maxInternedBytes = 64
	maxInternedName  = 1024
	maxInternedTotal = 1 << 20
	recentSlots      = 256
)

type recentValue struct {
	v lua.LValue
	s string
}

// recentKey holds a qualified attribute key; its bytes are counted in the interner budget.
type recentKey struct {
	key string
}

// interner reuses Lua strings for names and short values across the trees of one reader.
// A direct-mapped slot in front of the map serves repeated strings without hashing; a
// slot only references strings the map holds, and key slots are counted in the budget.
type interner struct {
	values map[string]lua.LValue
	recent [recentSlots]recentValue
	keys   [recentSlots]recentKey
	bytes  int
}

func newInterner() *interner { return &interner{values: make(map[string]lua.LValue)} }

func slotOf[T ~string | ~[]byte](s T) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s)*31 + int(s[0])*7 + int(s[len(s)-1])) & (recentSlots - 1)
}

// keep records s in the map while the entry and byte budgets allow it.
func (in *interner) keep(s string, v lua.LValue) bool {
	if len(in.values) < maxInterned && in.bytes+len(s) <= maxInternedTotal {
		in.values[s] = v
		in.bytes += len(s)
		return true
	}
	return false
}

// name returns the Lua string for an immutable name.
func (in *interner) name(s string) lua.LValue {
	if len(s) > maxInternedName {
		return lua.LString(s)
	}
	slot := &in.recent[slotOf(s)]
	if slot.v != nil && slot.s == s {
		return slot.v
	}
	v, ok := in.values[s]
	if !ok {
		v = lua.LString(s)
		if !in.keep(s, v) {
			return v
		}
	}
	*slot = recentValue{v: v, s: s}
	return v
}

// value returns the Lua string for borrowed bytes.
func (in *interner) value(p []byte) lua.LValue {
	if len(p) > maxInternedBytes {
		return lua.LString(p)
	}
	slot := &in.recent[slotOf(p)]
	if slot.v != nil && slot.s == string(p) {
		return slot.v
	}
	v, ok := in.values[string(p)]
	if !ok {
		s := string(p)
		v = lua.LString(s)
		if in.keep(s, v) {
			*slot = recentValue{v: v, s: s}
		}
		return v
	}
	*slot = recentValue{v: v, s: string(v.(lua.LString))}
	return v
}

// attrKey returns the Element.attrs key, reusing the key built for a qualified name.
func (in *interner) attrKey(ns, name string) string {
	size := len(ns) + len(name) + len("{}")
	if ns == "" || size > maxInternedName {
		return xml.AttrKey(ns, name)
	}
	slot := &in.keys[(slotOf(name)+len(ns))&(recentSlots-1)]
	if k := slot.key; len(k) == size && k[1:1+len(ns)] == ns && k[size-len(name):] == name {
		return k
	}
	key := xml.AttrKey(ns, name)
	in.bytes -= len(slot.key)
	slot.key = ""
	if in.bytes+size <= maxInternedTotal {
		slot.key = key
		in.bytes += size
	}
	return key
}

type treeFrame struct {
	table *lua.LTable
	text  []byte
	kids  int
}

// builder materializes Element tables from replayed events. Each completed root
// is appended to roots; comments and processing instructions are ignored.
type builder struct {
	in    *interner
	roots *lua.LTable
	stack []treeFrame
	kids  []lua.LValue
}

func (b *builder) push(t *lua.LTable) {
	n := len(b.stack)
	if n < cap(b.stack) {
		b.stack = b.stack[:n+1]
	} else {
		b.stack = append(b.stack, treeFrame{})
	}
	f := &b.stack[n]
	f.table = t
	f.text = f.text[:0]
	f.kids = len(b.kids)
}

// flushText moves pending text of the innermost element into its children.
func (b *builder) flushText(f *treeFrame) {
	if len(f.text) == 0 {
		return
	}
	b.kids = append(b.kids, b.in.value(f.text))
	f.text = f.text[:0]
}

func (b *builder) event(ev *xml.Event) error {
	switch ev.Kind {
	case xml.StartEvent:
		fields := 2
		if ev.NS != "" {
			fields++
		}
		if len(ev.Attrs) > 0 {
			fields++
		}
		if len(ev.XMLNS) > 0 {
			fields++
		}
		t := lua.CreateTable(0, fields)
		setField(t, "name", b.in.name(ev.Name))
		if ev.NS != "" {
			setField(t, "ns", b.in.name(ev.NS))
		}
		if len(ev.Attrs) > 0 {
			attrs := lua.CreateTable(0, len(ev.Attrs))
			for _, a := range ev.Attrs {
				setField(attrs, b.in.attrKey(a.NS, a.Name), b.in.value(a.Value))
			}
			setField(t, "attrs", attrs)
		}
		if len(ev.XMLNS) > 0 {
			xmlns := lua.CreateTable(0, len(ev.XMLNS))
			for _, d := range ev.XMLNS {
				setField(xmlns, d.Prefix, b.in.name(d.URI))
			}
			setField(t, "xmlns", xmlns)
		}
		if n := len(b.stack); n > 0 {
			parent := &b.stack[n-1]
			b.flushText(parent)
			b.kids = append(b.kids, t)
		} else {
			b.roots.Append(t)
		}
		b.push(t)
	case xml.TextEvent:
		if n := len(b.stack); n > 0 {
			b.stack[n-1].text = append(b.stack[n-1].text, ev.Text...)
		}
	case xml.EndEvent:
		f := &b.stack[len(b.stack)-1]
		if len(b.kids) == f.kids {
			if len(f.text) > 0 {
				setField(f.table, "text", b.in.value(f.text))
			}
		} else {
			b.flushText(f)
			children := lua.CreateTable(len(b.kids)-f.kids, 0)
			children.Array = append(children.Array, b.kids[f.kids:]...)
			clear(b.kids[f.kids:])
			b.kids = b.kids[:f.kids]
			setField(f.table, "children", children)
		}
		f.table = nil
		b.stack = b.stack[:len(b.stack)-1]
	}
	return nil
}

func newBuilder(in *interner, capacity int) *builder {
	return &builder{in: in, roots: lua.CreateTable(capacity, 0)}
}

// setField stores a field of a table created with string capacity. Fresh tables have
// no traversal state, so the direct write matches RawSetString without boxing the key.
func setField(t *lua.LTable, key string, v lua.LValue) { t.Strdict[key] = v }

// trees materializes every complete element of batch into a new array.
func trees(in *interner, batch *xml.Batch, capacity int) (*lua.LTable, error) {
	b := newBuilder(in, capacity)
	if err := batch.Replay(func(ev xml.Event) error { return b.event(&ev) }); err != nil {
		return nil, err
	}
	return b.roots, nil
}
