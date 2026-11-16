// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"io"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	xmlapi "github.com/wippyai/runtime/api/xml"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/wippyai/runtime/system/xml"
	"github.com/wippyai/runtime/system/xml/xmlcmd"
)

const writerTypeName = "xml.Writer"

var writerMetatable *lua.LTable

// Writer is the Lua handle of an XML writer. Output accumulates in the encoder; a
// stream writer yields to flush once the buffer reaches the threshold.
type Writer struct {
	entry  *xmlcmd.Writer
	table  *resource.Table
	attrs  []xml.AttrValue
	handle uint64
}

func writerFunc(l *lua.LState) int {
	opts, lerr := writeOptions(l, 2)
	if lerr != nil {
		return fail(l, lua.LNil, lerr)
	}
	table := tableOf(l)
	var dst io.Writer
	var sync func() error
	switch v := l.Get(1).(type) {
	case *lua.LNilType:
	case *lua.LUserData:
		if table == nil {
			return fail(l, lua.LNil, luaError(l, xmlapi.ErrNoTable))
		}
		switch s := v.Value.(type) {
		case *streammod.Stream:
			var err error
			if dst, sync, err = xmlcmd.StreamSink(table, s.ID); err != nil {
				return fail(l, lua.LNil, argument(l, "destination stream: "+err.Error()))
			}
		case io.Writer:
			dst = s
		default:
			return fail(l, lua.LNil, argument(l, "destination must be a writable stream"))
		}
	default:
		return fail(l, lua.LNil, argument(l, "destination must be a writable stream"))
	}
	entry, err := xmlcmd.NewWriter(opts, dst, sync)
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	w := &Writer{entry: entry}
	if table != nil {
		w.table = table
		w.handle = xmlcmd.InsertWriter(table, entry)
	}
	value.PushUserData(l, w, writerMetatable)
	l.Push(lua.LNil)
	return 2
}

func writerArg(l *lua.LState, n int) *Writer {
	if ud, ok := l.Get(n).(*lua.LUserData); ok {
		if w, ok := ud.Value.(*Writer); ok {
			return w
		}
	}
	raise(l, argument(l, "xml.Writer expected"))
	return nil
}

func (w *Writer) usable(l *lua.LState) *lua.Error {
	if err := w.entry.Err(); err != nil {
		return luaError(l, err)
	}
	return nil
}

// writerMethod guards a writer method: a closed or failed writer returns its terminal
// error before the method looks at its arguments.
func writerMethod(first lua.LValue, method func(*lua.LState, *Writer) int) lua.LGoFunc {
	return func(l *lua.LState) int {
		w := writerArg(l, 1)
		if err := w.usable(l); err != nil {
			return fail(l, first, err)
		}
		return method(l, w)
	}
}

// written completes a write: a full stream buffer is flushed through the dispatcher.
func (w *Writer) written(l *lua.LState, full bool) int {
	if full {
		l.Push(&flushYield{w: w})
		return -1
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// write runs op on the encoder under the writer's lock and completes the method.
func (w *Writer) write(l *lua.LState, op func(*xml.Encoder) error) int {
	full, err := w.entry.Write(op)
	if err != nil {
		return fail(l, lua.LFalse, luaError(l, err))
	}
	return w.written(l, full)
}

// tree bounds op as one tree by max_node_bytes.
func tree(op func(*xml.Encoder) error) func(*xml.Encoder) error {
	return func(e *xml.Encoder) error {
		e.BeginTree()
		defer e.EndTree()
		return op(e)
	}
}

// keptScratch is the attribute scratch capacity a writer keeps between calls: the default
// max_attrs, so a rejected oversized table does not stay allocated with the writer.
const keptScratch = 256

// releaseAttrs drops the references the attribute scratch holds after a call.
func (w *Writer) releaseAttrs() {
	clear(w.attrs)
	if cap(w.attrs) > keptScratch {
		w.attrs = nil
	}
}

// attrValues reads an optional attribute table keyed as in Element.attrs.
func (w *Writer) attrValues(l *lua.LState, n int) []xml.AttrValue {
	attrs := w.attrs[:0]
	defer func() { w.attrs = attrs }()
	switch v := l.Get(n).(type) {
	case *lua.LNilType:
	case *lua.LTable:
		for k, val := range tablePairs(v) {
			ks, ok1 := k.(lua.LString)
			vs, ok2 := val.(lua.LString)
			if !ok1 || !ok2 {
				raise(l, argument(l, "attrs must map strings to strings"))
			}
			attrs = append(attrs, xml.AttrValue{Key: string(ks), Value: string(vs)})
		}
	default:
		raise(l, argument(l, "attrs must be a table"))
	}
	return attrs
}

func checkWriter(l *lua.LState) *Writer { return writerArg(l, 1) }

func writerStartElement(l *lua.LState, w *Writer) int {
	defer w.releaseAttrs()
	name := stringArg(l, 2, "name")
	attrs := w.attrValues(l, 3)
	ns, _ := optionalString(l, 4, "ns")
	return w.write(l, func(e *xml.Encoder) error { return e.StartNode(name, ns, attrs, nil, false) })
}

func writerEndElement(l *lua.LState, w *Writer) int {
	return w.write(l, (*xml.Encoder).EndElement)
}

func writerText(l *lua.LState, w *Writer) int {
	data := stringArg(l, 2, "data")
	return w.write(l, func(e *xml.Encoder) error { return e.Text(data) })
}

func writerElement(l *lua.LState, w *Writer) int {
	defer w.releaseAttrs()
	name := stringArg(l, 2, "name")
	attrs := w.attrValues(l, 3)
	text, _ := optionalString(l, 4, "text")
	ns, _ := optionalString(l, 5, "ns")
	return w.write(l, tree(func(e *xml.Encoder) error {
		if err := e.StartNode(name, ns, attrs, nil, false); err != nil {
			return err
		}
		if err := e.Text(text); err != nil {
			return err
		}
		return e.EndElement()
	}))
}

func writerNode(l *lua.LState, w *Writer) int {
	element, ok := l.Get(2).(*lua.LTable)
	if !ok {
		return fail(l, lua.LFalse, argument(l, "element must be a table"))
	}
	return w.write(l, tree(func(e *xml.Encoder) error { return writeTree(e, element) }))
}

func writerComment(l *lua.LState, w *Writer) int {
	data := stringArg(l, 2, "data")
	return w.write(l, func(e *xml.Encoder) error { return e.Comment(data) })
}

func writerFlush(l *lua.LState, w *Writer) int {
	if !w.entry.Streaming() {
		return pushAll(l, boolResult(l, w.entry.Flush(opContext(l), false)))
	}
	l.Push(&flushYield{w: w, sync: true})
	return -1
}

func writerFinish(l *lua.LState, w *Writer) int {
	doc, err := w.entry.Finish()
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	if !w.entry.Streaming() {
		l.Push(lua.LString(doc))
		l.Push(lua.LNil)
		return 2
	}
	l.Push(&flushYield{w: w, sync: true, finish: true})
	return -1
}

func writerClose(l *lua.LState) int {
	w := checkWriter(l)
	w.attrs = nil
	w.entry.Close()
	if w.table != nil && w.handle != 0 {
		w.table.Remove(resource.Handle(w.handle))
		w.handle = 0
	}
	l.Push(lua.LTrue)
	return 1
}

var writerMethods = map[string]lua.LGoFunc{
	"start_element": writerMethod(lua.LFalse, writerStartElement),
	"end_element":   writerMethod(lua.LFalse, writerEndElement),
	"text":          writerMethod(lua.LFalse, writerText),
	"element":       writerMethod(lua.LFalse, writerElement),
	"node":          writerMethod(lua.LFalse, writerNode),
	"comment":       writerMethod(lua.LFalse, writerComment),
	"flush":         writerMethod(lua.LFalse, writerFlush),
	"finish":        writerMethod(lua.LNil, writerFinish),
	"close":         writerClose,
}
