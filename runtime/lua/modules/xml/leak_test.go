// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"io"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/system/xml/xmlcmd"
)

func countEntries(table *resource.Table) (readers, writers int) {
	table.Each(func(_ resource.Handle, typeID uint32, _ any) bool {
		switch typeID {
		case xmlcmd.TypeReader:
			readers++
		case xmlcmd.TypeWriter:
			writers++
		}
		return true
	})
	return readers, writers
}

func TestCloseReleasesTableEntries(t *testing.T) {
	e := newEnv(t)
	e.global("src", e.stream(strings.NewReader("<r><i/></r>")))
	sink, _ := e.sink(io.Discard)
	e.global("dest", sink)
	e.run(`readers = {check(xml.reader("<a/>")), check(xml.reader(src))}
		writers = {check(xml.writer()), check(xml.writer(dest))}`)
	r, w := countEntries(e.table)
	assert.Equal(t, 2, r)
	assert.Equal(t, 2, w)
	e.run(`for _, r in ipairs(readers) do r:close() end
		for _, w in ipairs(writers) do w:close() end
		local s = check(xml.reader("<r><i/></r>"))
		local records = check(s:select("r/i"))
		records:close()`)
	r, w = countEntries(e.table)
	assert.Zero(t, r)
	assert.Zero(t, w)
}

func TestFrameEndReleasesOpenHandles(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	mod, _ := Module.Build()
	l.SetGlobal("xml", mod)
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	l.SetContext(ctx)
	require.NoError(t, l.DoString(`r = xml.reader("<a><b/></a>")
		r:next()
		w = xml.writer()
		w:start_element("open")`))
	var entries []interface{ Closed() bool }
	store.Table().Each(func(_ resource.Handle, _ uint32, v any) bool {
		if c, ok := v.(interface{ Closed() bool }); ok {
			entries = append(entries, c)
		}
		return true
	})
	require.Len(t, entries, 2)
	require.NoError(t, store.Close())
	for _, c := range entries {
		assert.True(t, c.Closed(), "ending the frame releases readers and writers")
	}
	require.NoError(t, l.DoString(`local k, err = r:next()
		assert(k == nil and err:details().code == "state")
		local ok, werr = w:end_element()
		assert(ok == false and werr:details().code == "state")`))
}

func TestHandlesDoNotLeakUnderLoad(t *testing.T) {
	e := newEnv(t)
	doc := "<r>" + strings.Repeat(`<i v="x">text</i>`, 2000) + "</r>"
	e.global("doc", lua.LString(doc))
	e.global("open_stream", lua.LGoFunc(func(l *lua.LState) int {
		l.Push(e.stream(strings.NewReader(doc)))
		return 1
	}))
	e.global("open_sink", lua.LGoFunc(func(l *lua.LState) int {
		sink, _ := e.sink(io.Discard)
		l.Push(sink)
		return 1
	}))
	cycle := func(n int) {
		e.run(`for i = 1, ` + strconv.Itoa(n) + ` do
			local r = check(xml.reader(i % 2 == 0 and doc or open_stream()))
			local w = check(xml.writer(open_sink()))
			r:next(); r:next()
			check(r:copy(w))
			r:close()
			w:close()
			local s = check(xml.reader(doc))
			local records = check(s:select("r/i"))
			check(records:read(10))
			records:close()
		end`)
	}
	cycle(200)
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	cycle(2000)
	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	r, w := countEntries(e.table)
	assert.Zero(t, r)
	assert.Zero(t, w)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("heap growth after 2000 cycles: %d KiB", growth>>10)
	assert.Less(t, growth, int64(4<<20), "closed handles release their buffers")
}
