// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"errors"
	"io"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/system/xml"
)

const (
	recordsTypeName = "xml.Records"
	defaultRecords  = 64
	recordsCapacity = 256
)

var recordsMetatable *lua.LTable

// Records is the Lua handle of a record selection; it owns its reader.
type Records struct {
	reader *Reader
}

func checkRecords(l *lua.LState) *Records {
	if ud, ok := l.Get(1).(*lua.LUserData); ok {
		if r, ok := ud.Value.(*Records); ok {
			return r
		}
	}
	raise(l, argument(l, "xml.Records expected"))
	return nil
}

func recordsRead(l *lua.LState) int {
	rs := checkRecords(l)
	r := rs.reader
	if err := r.entry.Err(); err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	if r.entry.Closed() {
		return fail(l, lua.LNil, state(l, "records are closed"))
	}
	n := defaultRecords
	switch v := l.Get(2).(type) {
	case *lua.LNilType:
	case lua.LInteger:
		n = int(v)
	case lua.LNumber:
		n = int(v)
		if lua.LNumber(n) != v {
			return fail(l, lua.LNil, argument(l, "n must be an integer"))
		}
	default:
		return fail(l, lua.LNil, argument(l, "n must be an integer"))
	}
	if n < 1 {
		return fail(l, lua.LNil, argument(l, "n must be positive"))
	}
	if r.stream {
		l.Push(&recordsYield{r: r, n: n})
		return -1
	}
	_, err := r.entry.ReadRecords(opContext(l), n)
	return pushAll(l, recordsResult(l, r, n, err))
}

func recordsResult(l *lua.LState, r *Reader, n int, err error) []lua.LValue {
	if errors.Is(err, io.EOF) {
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	if err != nil {
		return []lua.LValue{lua.LNil, luaError(l, err)}
	}
	var roots *lua.LTable
	err = r.entry.ViewRecords(func(b *xml.Batch) error {
		var terr error
		roots, terr = trees(r.in, b, min(n, recordsCapacity))
		return terr
	})
	if err != nil {
		return []lua.LValue{lua.LNil, luaError(l, err)}
	}
	return []lua.LValue{roots, lua.LNil}
}

func recordsClose(l *lua.LState) int {
	checkRecords(l).reader.close()
	l.Push(lua.LTrue)
	return 1
}

var recordsMethods = map[string]lua.LGoFunc{
	"read":  recordsRead,
	"close": recordsClose,
}

func readerSelect(l *lua.LState) int {
	r := checkReader(l)
	if err := r.cursorError(l); err != nil {
		return fail(l, lua.LNil, err)
	}
	var sources []string
	switch v := l.Get(2).(type) {
	case lua.LString:
		sources = []string{string(v)}
	case *lua.LTable:
		for _, p := range v.Array[:v.Len()] {
			s, ok := p.(lua.LString)
			if !ok {
				return fail(l, lua.LNil, argument(l, "paths must be strings"))
			}
			sources = append(sources, string(s))
		}
		if len(sources) == 0 || len(v.Strdict) > 0 || len(v.Dict) > 0 {
			return fail(l, lua.LNil, argument(l, "paths must be a non-empty array of strings"))
		}
	default:
		return fail(l, lua.LNil, argument(l, "paths must be a string or an array of strings"))
	}
	ns, lerr := selectOptions(l, 3)
	if lerr != nil {
		return fail(l, lua.LNil, lerr)
	}
	paths := make([]xml.Path, len(sources))
	for i, s := range sources {
		p, err := xml.CompilePath(s, ns)
		if err != nil {
			return fail(l, lua.LNil, luaError(l, err))
		}
		paths[i] = p
	}
	if err := r.entry.Select(paths); err != nil {
		return fail(l, lua.LNil, luaError(l, err))
	}
	value.PushUserData(l, &Records{reader: r}, recordsMetatable)
	l.Push(lua.LNil)
	return 2
}
