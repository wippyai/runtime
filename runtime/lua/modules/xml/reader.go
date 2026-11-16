// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"errors"
	"io"
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	xmlapi "github.com/wippyai/runtime/api/xml"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/system/xml"
	"github.com/wippyai/runtime/system/xml/xmlcmd"
)

const readerTypeName = "xml.Reader"

var readerMetatable *lua.LTable

// Reader is the Lua handle of an XML reader. A string source is decoded on the VM
// thread; a stream source reads through the dispatcher whenever input is needed.
type Reader struct {
	entry  *xmlcmd.Reader
	table  *resource.Table
	in     *interner
	handle uint64
	stream bool
}

var kindValues = [...]lua.LValue{
	xml.StartEvent:   lua.LString("start"),
	xml.EndEvent:     lua.LString("end"),
	xml.TextEvent:    lua.LString("text"),
	xml.CommentEvent: lua.LString("comment"),
	xml.PIEvent:      lua.LString("pi"),
}

func kindValue(k xml.Kind) lua.LValue {
	if k == 0 || int(k) >= len(kindValues) {
		return lua.LNil
	}
	return kindValues[k]
}

func readerFunc(l *lua.LState) int {
	opts, lerr := readOptions(l, 2)
	if lerr != nil {
		return fail(l, lua.LNil, lerr)
	}
	table := tableOf(l)
	var src io.Reader
	stream := false
	switch v := l.Get(1).(type) {
	case lua.LString:
		src = strings.NewReader(string(v))
	case *lua.LUserData:
		if src, lerr = streamSource(l, v, table); lerr != nil {
			return fail(l, lua.LNil, lerr)
		}
		stream = true
	default:
		return fail(l, lua.LNil, argument(l, "source must be a string or a readable stream"))
	}
	entry, err := xmlcmd.NewReader(opContext(l), src, opts)
	if err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	r := &Reader{entry: entry, in: newInterner(), stream: stream}
	if table != nil {
		r.table = table
		r.handle = xmlcmd.InsertReader(table, entry)
	}
	value.PushUserData(l, r, readerMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkReader(l *lua.LState) *Reader {
	if ud, ok := l.Get(1).(*lua.LUserData); ok {
		if r, ok := ud.Value.(*Reader); ok {
			return r
		}
	}
	raise(l, argument(l, "xml.Reader expected"))
	return nil
}

// cursorError reports why cursor methods are unavailable, or nil. The order is the one
// every reader, records and writer method follows: the failure that made it terminal,
// then closed, then owned by a record selection.
func (r *Reader) cursorError(l *lua.LState) *lua.Error {
	if err := r.entry.Err(); err != nil {
		return luaError(l, err)
	}
	if r.entry.Closed() {
		return state(l, "reader is closed")
	}
	if r.entry.Selected() {
		return state(l, "record selection owns the reader")
	}
	return nil
}

// usable reports whether accessors may report events: the reader has not failed, is not
// closed and is not owned by a record selection.
func (r *Reader) usable() bool {
	return r.entry.Cursor().Err() == nil && !r.entry.Closed() && !r.entry.Selected()
}

// current returns the current event, or nil when the reader is not usable.
func (r *Reader) current() *xml.Event {
	if !r.usable() {
		return nil
	}
	ev := r.entry.Cursor().Current()
	if ev.Kind == 0 {
		return nil
	}
	return ev
}

func (r *Reader) close() {
	r.entry.Close()
	r.in = nil
	if r.table != nil && r.handle != 0 {
		r.table.Remove(resource.Handle(r.handle))
		r.handle = 0
	}
}

// dispatched reports whether an operation must run on the dispatcher.
func (r *Reader) dispatched(buffered bool) bool { return r.stream && !buffered }

func readerNext(l *lua.LState) int {
	r := checkReader(l)
	if err := r.cursorError(l); err != nil {
		return fail(l, lua.LNil, err)
	}
	c := r.entry.Cursor()
	if ev, ok := c.Next(); ok {
		l.Push(kindValues[ev.Kind])
		l.Push(lua.LNil)
		return 2
	}
	if r.stream && c.NeedsInput() {
		l.Push(&fillYield{r: r})
		return -1
	}
	return pushAll(l, nextResult(l, r, r.entry.Fill(opContext(l))))
}

// nextResult converts the outcome of a fill into the results of next().
func nextResult(l *lua.LState, r *Reader, err error) []lua.LValue {
	if errors.Is(err, io.EOF) {
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	if err != nil {
		return []lua.LValue{lua.LNil, luaError(l, err)}
	}
	if ev, ok := r.entry.Cursor().Next(); ok {
		return []lua.LValue{kindValues[ev.Kind], lua.LNil}
	}
	return []lua.LValue{lua.LNil, lua.LNil}
}

func pushAll(l *lua.LState, values []lua.LValue) int {
	for _, v := range values {
		l.Push(v)
	}
	return len(values)
}

func readerKind(l *lua.LState) int {
	r := checkReader(l)
	if ev := r.current(); ev != nil {
		l.Push(kindValue(ev.Kind))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func readerName(l *lua.LState) int {
	r := checkReader(l)
	if ev := r.current(); ev != nil && (ev.Kind == xml.StartEvent || ev.Kind == xml.EndEvent || ev.Kind == xml.PIEvent) {
		l.Push(r.in.name(ev.Name))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func readerNS(l *lua.LState) int {
	r := checkReader(l)
	if ev := r.current(); ev != nil && (ev.Kind == xml.StartEvent || ev.Kind == xml.EndEvent) && ev.NS != "" {
		l.Push(r.in.name(ev.NS))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func readerAttr(l *lua.LState) int {
	r := checkReader(l)
	if !r.usable() {
		l.Push(lua.LNil)
		return 1
	}
	name := stringArg(l, 2, "name")
	ns, _ := optionalString(l, 3, "ns")
	if ev := r.current(); ev != nil && ev.Kind == xml.StartEvent {
		for _, a := range ev.Attrs {
			if a.Name == name && a.NS == ns {
				l.Push(r.in.value(a.Value))
				return 1
			}
		}
	}
	l.Push(lua.LNil)
	return 1
}

func readerAttrs(l *lua.LState) int {
	r := checkReader(l)
	ev := r.current()
	if ev == nil || ev.Kind != xml.StartEvent {
		l.Push(lua.CreateTable(0, 0))
		return 1
	}
	t := lua.CreateTable(0, len(ev.Attrs))
	for _, a := range ev.Attrs {
		t.RawSetString(xml.AttrKey(a.NS, a.Name), r.in.value(a.Value))
	}
	l.Push(t)
	return 1
}

func readerText(l *lua.LState) int {
	r := checkReader(l)
	if ev := r.current(); ev != nil && (ev.Kind == xml.TextEvent || ev.Kind == xml.CommentEvent || ev.Kind == xml.PIEvent) {
		l.Push(lua.LString(ev.Text))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func readerDepth(l *lua.LState) int {
	r := checkReader(l)
	depth := 0
	if ev := r.current(); ev != nil {
		depth = ev.Depth
	}
	l.Push(lua.LInteger(depth))
	return 1
}

func readerPos(l *lua.LState) int {
	r := checkReader(l)
	var pos xml.Position
	if ev := r.current(); ev != nil {
		pos = ev.Pos
	}
	t := lua.CreateTable(0, 3)
	t.RawSetString("line", lua.LInteger(pos.Line))
	t.RawSetString("column", lua.LInteger(pos.Column))
	t.RawSetString("offset", lua.LInteger(pos.Offset))
	l.Push(t)
	return 1
}

// startError reports why an element operation cannot run, or nil.
func (r *Reader) startError(l *lua.LState, what string) *lua.Error {
	if err := r.cursorError(l); err != nil {
		return err
	}
	if r.entry.Cursor().Current().Kind != xml.StartEvent {
		return state(l, what+" requires a start event")
	}
	return nil
}

func readerNode(l *lua.LState) int {
	r := checkReader(l)
	if err := r.startError(l, "node"); err != nil {
		return fail(l, lua.LNil, err)
	}
	if r.dispatched(r.entry.Cursor().Buffered()) {
		l.Push(&nodeYield{r: r})
		return -1
	}
	return pushAll(l, nodeResult(l, r, r.entry.Node(opContext(l))))
}

func nodeResult(l *lua.LState, r *Reader, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, luaError(l, err)}
	}
	var roots *lua.LTable
	err = r.entry.ViewNode(func(b *xml.Batch) error {
		var terr error
		roots, terr = trees(r.in, b, 1)
		return terr
	})
	if err != nil {
		return []lua.LValue{lua.LNil, luaError(l, err)}
	}
	return []lua.LValue{roots.RawGetInt(1), lua.LNil}
}

func boolResult(l *lua.LState, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, luaError(l, err)}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

func readerSkip(l *lua.LState) int {
	r := checkReader(l)
	if err := r.startError(l, "skip"); err != nil {
		return fail(l, lua.LFalse, err)
	}
	if r.dispatched(r.entry.Cursor().Buffered()) {
		l.Push(&skipYield{r: r})
		return -1
	}
	return pushAll(l, boolResult(l, r.entry.Skip(opContext(l))))
}

func readerCopy(l *lua.LState) int {
	r := checkReader(l)
	if err := r.cursorError(l); err != nil {
		return fail(l, lua.LFalse, err)
	}
	w := writerArg(l, 2)
	if err := r.startError(l, "copy"); err != nil {
		return fail(l, lua.LFalse, err)
	}
	if err := w.usable(l); err != nil {
		return fail(l, lua.LFalse, err)
	}
	if r.entry.Cursor().Buffered() || !r.stream && !w.entry.Streaming() {
		full, err := r.entry.CopyBuffered(opContext(l), w.entry)
		if err != nil {
			return fail(l, lua.LFalse, luaError(l, err))
		}
		if full {
			l.Push(&flushYield{w: w, copier: r})
			return -1
		}
		l.Push(lua.LTrue)
		l.Push(lua.LNil)
		return 2
	}
	if r.handle == 0 || w.handle == 0 {
		return fail(l, lua.LFalse, luaError(l, xmlapi.ErrNoTable))
	}
	l.Push(&copyYield{r: r, w: w})
	return -1
}

func readerClose(l *lua.LState) int {
	checkReader(l).close()
	l.Push(lua.LTrue)
	return 1
}

var readerMethods = map[string]lua.LGoFunc{
	"select": readerSelect,
	"next":   readerNext,
	"kind":   readerKind,
	"name":   readerName,
	"ns":     readerNS,
	"attr":   readerAttr,
	"attrs":  readerAttrs,
	"text":   readerText,
	"depth":  readerDepth,
	"pos":    readerPos,
	"node":   readerNode,
	"skip":   readerSkip,
	"copy":   readerCopy,
	"close":  readerClose,
}
