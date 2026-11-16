// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	xmlapi "github.com/wippyai/runtime/api/xml"
	"github.com/wippyai/runtime/system/xml"
)

func TestMemoryWriter(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local w = check(xml.writer(nil, {declaration = true, indent = "  ", ns = {s = "urn:s"}}))
		check(w:start_element("catalog", {version = "2"}, "urn:s"))
		check(w:comment(" items "))
		check(w:element("item", {id = "1", ["{urn:x}k"] = "v"}, "a < b", "urn:s"))
		check(w:element("empty"))
		check(w:start_element("mixed"))
		check(w:text("x"))
		check(w:element("b", nil, "y"))
		check(w:end_element())
		check(w:node({name = "tree", children = {{name = "leaf", text = "z"}}}))
		check(w:end_element())
		eq(w:flush(), true, "flush of an in-memory writer")
		local doc, err = w:finish()
		check(err == nil, tostring(err))
		local _, again = w:finish()
		eq(code(again), "state", "finish twice")
		local _, after = w:text("late")
		eq(code(after), "state", "write after finish")
		eq(w:close(), true)
		eq(w:close(), true)
		local _, closed = w:start_element("x")
		eq(code(closed), "state", "write after close")
		return doc`)
	assert.Equal(t, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<s:catalog xmlns:s="urn:s" version="2"><!-- items -->`+"\n"+
		`  <s:item xmlns:ns1="urn:x" id="1" ns1:k="v">a &lt; b</s:item>`+"\n"+
		`  <empty/>`+"\n"+
		`  <mixed>x<b>y</b></mixed>`+"\n"+
		`  <tree>`+"\n    <leaf>z</leaf>\n  </tree>\n</s:catalog>", vals[0].String())
	assert.Zero(t, e.yieldCount(), "an in-memory writer never yields")
}

func TestWriterStateErrors(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local out = {}
		local w = check(xml.writer())
		local _, err = w:end_element(); out[#out + 1] = code(err)
		w = check(xml.writer())
		_, err = w:text("x"); out[#out + 1] = code(err)
		w = check(xml.writer())
		_, err = w:finish(); out[#out + 1] = code(err)
		w = check(xml.writer())
		w:start_element("a")
		_, err = w:finish(); out[#out + 1] = code(err)
		w = check(xml.writer())
		w:element("a")
		_, err = w:element("b"); out[#out + 1] = code(err)
		w = check(xml.writer())
		_, err = w:start_element("1a"); out[#out + 1] = code(err)
		local _, later = w:start_element("ok"); out[#out + 1] = code(later)
		w = check(xml.writer())
		_, err = w:comment("a--b"); out[#out + 1] = code(err)
		w = check(xml.writer())
		_, err = w:node({name = "a", attrs = {}}); out[#out + 1] = code(err)
		w = check(xml.writer())
		_, err = w:node("a"); out[#out + 1] = code(err)
		local ok, perr = pcall(function() w:start_element("a", {x = 1}) end)
		out[#out + 1] = code(perr)
		return table.concat(out, ",")`)
	assert.Equal(t, "state,state,state,state,state,element,element,syntax,element,argument,argument", vals[0].String())
}

// countingWriter records the size of every write it receives.
type countingWriter struct {
	fail   error
	writes []int
	buf    bytes.Buffer
	limit  int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c.fail != nil {
		return 0, c.fail
	}
	if c.limit > 0 && len(p) > c.limit {
		p = p[:c.limit]
	}
	c.writes = append(c.writes, len(p))
	return c.buf.Write(p)
}

func TestStreamWriterFlushThreshold(t *testing.T) {
	e := newEnv(t)
	dst := &countingWriter{limit: 7000}
	sink, s := e.sink(dst)
	e.global("dest", sink)
	vals := e.run(`local w = check(xml.writer(dest))
		check(w:start_element("feed"))
		for i = 1, 20000 do
			local ok, err = w:element("item", {id = tostring(i)}, "value " .. i)
			check(ok, tostring(err))
		end
		check(w:end_element())
		local doc, err = w:finish()
		check(err == nil, tostring(err))
		eq(doc, nil, "a stream writer returns no document")
		w:close()
		return true`)
	assert.Equal(t, "true", vals[0].String())
	out := dst.buf.String()
	assert.True(t, strings.HasPrefix(out, `<feed><item id="1">value 1</item>`))
	assert.True(t, strings.HasSuffix(out, `<item id="20000">value 20000</item></feed>`))
	_, err := decodeCheck(out)
	require.NoError(t, err)
	flushes := e.yields[xmlapi.WriterFlush]
	assert.Greater(t, flushes, 5)
	assert.Less(t, flushes, len(out)/(60<<10)+3, "methods yield only when the buffer reaches the threshold")
	assert.False(t, s.closed, "the writer leaves the stream open")
	for _, n := range dst.writes {
		assert.LessOrEqual(t, n, 7000)
	}
}

func decodeCheck(doc string) (string, error) {
	l := lua.NewState()
	defer l.Close()
	mod, _ := Module.Build()
	l.SetGlobal("xml", mod)
	l.SetGlobal("doc", lua.LString(doc))
	if err := l.DoString(`local _, err = xml.decode(doc); if err then error(err) end`); err != nil {
		return "", err
	}
	return doc, nil
}

func TestStreamWriterBoundedBuffer(t *testing.T) {
	e := newEnv(t)
	dst := &countingWriter{}
	sink, _ := e.sink(dst)
	e.global("dest", sink)
	var maxBuffered int
	e.global("probe", lua.LGoFunc(func(l *lua.LState) int {
		w := l.CheckUserData(1).Value.(*Writer)
		_, _ = w.entry.Write(func(e *xml.Encoder) error {
			maxBuffered = max(maxBuffered, len(e.Buffered()))
			return nil
		})
		return 0
	}))
	e.run(`local w = check(xml.writer(dest))
		w:start_element("feed")
		local text = string.rep("x", 1000)
		for i = 1, 5000 do
			check(w:element("item", nil, text))
			probe(w)
		end
		w:end_element()
		w:finish()`)
	assert.Less(t, maxBuffered, 64<<10+2048, "the buffer never grows past the threshold plus one write")
	assert.Greater(t, dst.buf.Len(), 5000*1000)
}

func TestStreamWriterFailureIsTerminal(t *testing.T) {
	e := newEnv(t)
	dst := &countingWriter{fail: errors.New("disk full")}
	sink, _ := e.sink(dst)
	e.global("dest", sink)
	vals := e.run(`local w = check(xml.writer(dest))
		w:start_element("a")
		local ok, err = w:flush()
		eq(ok, false)
		local _, again = w:text("x")
		local _, fin = w:finish()
		return code(err), err:kind(), err:retryable(), code(again), again:message() == err:message(), code(fin)`)
	assert.Equal(t, "io", vals[0].String())
	assert.Equal(t, "Internal", vals[1].String())
	assert.Equal(t, "false", vals[2].String())
	assert.Equal(t, "io", vals[3].String())
	assert.Equal(t, "true", vals[4].String())
	assert.Equal(t, "io", vals[5].String())
}

func TestWriterDestinationErrors(t *testing.T) {
	e := newEnv(t)
	e.global("src", e.stream(strings.NewReader("read only")))
	vals := e.run(`local _, err = xml.writer(src)
		local _, terr = xml.writer({})
		return code(err), code(terr)`)
	assert.Equal(t, "argument", vals[0].String())
	assert.Equal(t, "argument", vals[1].String())
}

func TestCopyCombinations(t *testing.T) {
	doc := `<r xmlns="urn:d" xmlns:p="urn:p"><keep id="1"><p:a>` + strings.Repeat("<x>text &amp; more</x>", 6000) +
		`</p:a></keep><drop id="2"><y/></drop><keep id="3"/></r>`
	e := newEnv(t)
	var outputs []string
	for _, source := range []string{"string", "stream", "tiny"} {
		for _, dest := range []string{"memory", "stream"} {
			e.global("src", sources(e, doc)[source]())
			var dst *countingWriter
			if dest == "memory" {
				e.global("dest", lua.LNil)
			} else {
				dst = &countingWriter{}
				sink, _ := e.sink(dst)
				e.global("dest", sink)
			}
			vals := e.run(`local r = check(xml.reader(src))
				local w = check(xml.writer(dest, {indent = "  "}))
				check(w:start_element("items"))
				while true do
					local kind, err = r:next()
					check(err == nil, tostring(err))
					if not kind then break end
					if kind == "start" and r:depth() == 2 then
						if r:name() == "keep" then
							check(r:copy(w))
						else
							check(r:skip())
						end
						eq(r:kind(), "end", "copy leaves the cursor on the end event")
					end
				end
				check(w:end_element())
				local out, err = w:finish()
				check(err == nil, tostring(err))
				return out`)
			out := vals[0].String()
			if dst != nil {
				out = dst.buf.String()
			}
			outputs = append(outputs, out)
		}
	}
	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i], "combination %d", i)
	}
	assert.True(t, strings.HasPrefix(outputs[0], "<items>\n  <keep xmlns=\"urn:d\" xmlns:p=\"urn:p\" id=\"1\">\n    <p:a>\n      <x>text &amp; more</x>"), outputs[0][:120])
	assert.True(t, strings.HasSuffix(outputs[0], "<keep xmlns=\"urn:d\" xmlns:p=\"urn:p\" id=\"3\"/>\n</items>"))
	assert.NotContains(t, outputs[0], "drop")
}

func TestCopyToClosedWriter(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local r = check(xml.reader("<a/>"))
		local w = check(xml.writer())
		w:close()
		r:next()
		local _, err = r:copy(w)
		local ok, perr = pcall(function() r:copy("x") end)
		return code(err), code(perr)`)
	assert.Equal(t, "state", vals[0].String())
	assert.Equal(t, "argument", vals[1].String())
}
