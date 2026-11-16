// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	xmlapi "github.com/wippyai/runtime/api/xml"
)

// tinyReader returns at most n bytes per read.
type tinyReader struct {
	r io.Reader
	n int
}

func (t *tinyReader) Read(p []byte) (int, error) {
	if len(p) > t.n {
		p = p[:t.n]
	}
	return t.r.Read(p)
}

const transcriptLua = `
function transcript(r)
	local out = {}
	while true do
		local kind, err = r:next()
		check(err == nil, tostring(err))
		if not kind then break end
		eq(r:kind(), kind, "kind")
		local p = r:pos()
		local attrs = r:attrs()
		local keys = {}
		for k, v in pairs(attrs) do keys[#keys + 1] = k .. "=" .. v end
		table.sort(keys)
		out[#out + 1] = table.concat({kind, r:name() or "", r:ns() or "", r:text() or "", r:depth(),
			p.line, p.column, p.offset, table.concat(keys, " "), r:attr("id") or ""}, "|")
	end
	check(r:kind() == nil and r:name() == nil and r:text() == nil, "nothing current at the end")
	local again, err = r:next()
	check(again == nil and err == nil, "end is stable")
	return table.concat(out, "\n")
end
`

const readerDocument = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
	`<!--lead--><cat:catalog xmlns:cat="urn:cat" xmlns="urn:d" version="2">` + "\n" +
	`  <item id="1" cat:code="A&amp;B">First &lt;item&gt;<![CDATA[ raw <x> ]]></item>` + "\n" +
	`  <?render fast?>` + "\n" +
	`  <item id="2"><name>Second</name><tags><tag>t1</tag><tag>t2</tag></tags></item>` + "\n" +
	`  <other xmlns=""><item id="3">` + "caf\u00e9 \U0001F600" + `</item></other>` + "\n" +
	`</cat:catalog>` + "\n"

func sources(e *env, doc string) map[string]func() lua.LValue {
	return map[string]func() lua.LValue{
		"string": func() lua.LValue { return lua.LString(doc) },
		"stream": func() lua.LValue { return e.stream(strings.NewReader(doc)) },
		"tiny":   func() lua.LValue { return e.stream(&tinyReader{r: strings.NewReader(doc), n: 3}) },
	}
}

func TestReaderCursorAcrossSources(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.l.DoString(transcriptLua))
	results := map[string]string{}
	for name, src := range sources(e, readerDocument) {
		e.global("src", src())
		vals := e.run(`local r = check(xml.reader(src, {comments = true, instructions = true}))
			local out = transcript(r)
			eq(r:close(), true, "close")
			return out`)
		results[name] = vals[0].String()
	}
	assert.Equal(t, results["string"], results["stream"])
	assert.Equal(t, results["string"], results["tiny"])
	lines := strings.Split(results["string"], "\n")
	assert.Equal(t, "comment||| lead |0|2|1|39||", strings.Replace(lines[0], "lead", " lead ", 1))
	assert.Equal(t, `start|catalog|urn:cat||1|2|12|50|version=2|`, lines[1])
	assert.Contains(t, results["string"], "text|||\n  |1|2|71|109||")
	assert.Contains(t, results["string"], `start|item|urn:d||2|3|3|112|id=1 {urn:cat}code=A&B|1`)
	assert.Contains(t, results["string"], "text|||First <item>|2|3|35|144||\ntext||| raw <x> |2|3|62|171||")
	assert.Contains(t, results["string"], "pi|render||fast|1|")
	assert.Contains(t, results["string"], "text|||café \U0001F600|3|")
	assert.Contains(t, results["string"], "end|catalog|urn:cat||0|")
}

func TestReaderWhitespaceStrip(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.l.DoString(transcriptLua))
	vals := e.run(`local r = check(xml.reader("<a>\n  <b> x </b>\n</a>", {whitespace = "strip"}))
		return transcript(r)`)
	assert.Equal(t, "start|a|||1|1|1|0||\nstart|b|||2|2|3|6||\ntext||| x |2|2|6|9||\nend|b|||1|2|9|12||\nend|a|||0|3|1|17||", vals[0].String())
}

func TestReaderTextChunks(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local r = check(xml.reader("<a>` + strings.Repeat("é", 10) + `</a>", {max_token_bytes = 5}))
		local chunks = {}
		while true do
			local kind = r:next()
			if not kind then break end
			if kind == "text" then chunks[#chunks + 1] = r:text() end
		end
		return #chunks, table.concat(chunks)`)
	assert.Equal(t, "5", vals[0].String(), "split on character boundaries")
	assert.Equal(t, strings.Repeat("é", 10), vals[1].String())
}

const positioningLua = `
local r = check(xml.reader(src, {whitespace = "strip"}))
local out = {}
while true do
	local kind, err = r:next()
	check(err == nil, tostring(err))
	if not kind then break end
	if kind == "start" and r:depth() == 2 then
		local id = r:attr("id")
		if id == "1" then
			local node, nerr = r:node()
			check(nerr == nil, tostring(nerr))
			out[#out + 1] = "node:" .. dump(node)
		elseif id == "2" then
			local ok, serr = r:skip()
			check(ok and serr == nil, tostring(serr))
			out[#out + 1] = "skip"
		else
			local ok, cerr = r:copy(w)
			check(ok and cerr == nil, tostring(cerr))
			out[#out + 1] = "copy"
		end
		out[#out + 1] = "at:" .. r:kind() .. ":" .. r:name() .. ":" .. r:depth()
	end
end
local doc, ferr = w:finish()
check(ferr == nil, tostring(ferr))
return table.concat(out, "\n"), doc
`

func TestReaderPositioningAfterElementOperations(t *testing.T) {
	doc := `<r xmlns:p="urn:p"><i id="1" p:k="v"><n>one</n>` + strings.Repeat("<pad/>", 10000) + `</i>` +
		`<i id="2">` + strings.Repeat("<pad>x</pad>", 10000) + `</i><i id="3"><p:z>three</p:z></i><i id="4"/></r>`
	e := newEnv(t)
	var outputs []string
	for _, name := range []string{"string", "stream", "tiny"} {
		e.global("src", sources(e, doc)[name]())
		vals := e.run(`w = check(xml.writer())
			check(w:start_element("out"))` + strings.Replace(positioningLua, "local doc, ferr = w:finish()", `check(w:end_element())
			local doc, ferr = w:finish()`, 1))
		outputs = append(outputs, vals[0].String()+"\n"+vals[1].String())
	}
	assert.Equal(t, outputs[0], outputs[1])
	assert.Equal(t, outputs[0], outputs[2])
	lines := strings.Split(outputs[0], "\n")
	assert.True(t, strings.HasPrefix(lines[0], `node:i[id="1" {urn:p}k="v"](p="urn:p")<n"one",pad,`), lines[0])
	assert.Equal(t, "at:end:i:1", lines[1])
	assert.Equal(t, "skip", lines[2])
	assert.Equal(t, "at:end:i:1", lines[3])
	assert.Equal(t, `<out><i xmlns:p="urn:p" id="3"><p:z>three</p:z></i><i xmlns:p="urn:p" id="4"/></out>`, lines[len(lines)-1])
}

func TestReaderElementOperationsRequireStart(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local r = check(xml.reader("<a>text</a>"))
		local w = check(xml.writer())
		local _, e1 = r:node()
		local _, e2 = r:skip()
		local _, e3 = r:copy(w)
		r:next(); r:next()
		eq(r:kind(), "text")
		local _, e4 = r:node()
		eq(r:next(), "end", "a state error does not break the reader")
		return code(e1), code(e2), code(e3), code(e4)`)
	for _, v := range vals {
		assert.Equal(t, "state", v.String())
	}
}

func TestReaderCloseAndUseAfterClose(t *testing.T) {
	e := newEnv(t)
	for name, src := range sources(e, "<a><b/></a>") {
		t.Run(name, func(t *testing.T) {
			e.global("src", src())
			before := e.table.Len()
			vals := e.run(`local r = check(xml.reader(src))
				eq(r:next(), "start")
				eq(r:close(), true)
				eq(r:close(), true, "close is idempotent")
				local kind, err = r:next()
				check(kind == nil, "no event after close")
				local _, nerr = r:node()
				local _, serr = r:select("a")
				eq(r:kind(), nil)
				eq(r:depth(), 0)
				return code(err), code(nerr), code(serr), err:kind()`)
			assert.Equal(t, []string{"state", "state", "state", "Invalid"}, []string{vals[0].String(), vals[1].String(), vals[2].String(), vals[3].String()})
			assert.LessOrEqual(t, e.table.Len(), before, "closing removes the reader from the resource table")
		})
	}
}

func TestReaderLeavesStreamOpen(t *testing.T) {
	e := newEnv(t)
	stream := e.stream(strings.NewReader("<a/><!-- tail -->"))
	e.global("src", stream)
	vals := e.run(`local r = check(xml.reader(src))
		eq(r:next(), "start")
		r:close()
		local rest, err = src:read()
		return rest, err`)
	assert.Equal(t, lua.LTNil, vals[1].Type(), "the stream is still open after the reader closes")
}

func TestRecords(t *testing.T) {
	doc := `<s:catalog xmlns:s="urn:shop" xmlns:xsi="urn:xsi"><s:item id="1" xsi:type="s:Book"><title>A</title></s:item>` +
		`<other/><s:item id="2"><s:item id="nested"/></s:item><s:meta n="m"/><s:item id="3"/></s:catalog>`
	e := newEnv(t)
	var outputs []string
	for _, name := range []string{"string", "stream", "tiny"} {
		e.global("src", sources(e, doc)[name]())
		vals := e.run(`local r = check(xml.reader(src))
			local records = check(r:select({"s:catalog/s:item", "/s:catalog/s:meta"}, {ns = {s = "urn:shop"}}))
			local _, cerr = r:next()
			eq(code(cerr), "state", "selection owns the reader")
			local out = {}
			while true do
				local batch, err = records:read(2)
				check(err == nil, tostring(err))
				if not batch then break end
				check(#batch <= 2, "batch size")
				for _, rec in ipairs(batch) do out[#out + 1] = dump(rec) end
				out[#out + 1] = "|"
			end
			local again, aerr = records:read()
			check(again == nil and aerr == nil, "end is stable")
			eq(records:close(), true)
			eq(records:close(), true)
			local _, rerr = records:read()
			eq(code(rerr), "state", "read after close")
			return table.concat(out, "\n")`)
		outputs = append(outputs, vals[0].String())
	}
	assert.Equal(t, outputs[0], outputs[1])
	assert.Equal(t, outputs[0], outputs[2])
	assert.Equal(t, `item{urn:shop}[id="1" {urn:xsi}type="s:Book"](s="urn:shop" xsi="urn:xsi")<title"A">`+"\n"+
		`item{urn:shop}[id="2"](s="urn:shop" xsi="urn:xsi")<item{urn:shop}[id="nested"]>`+"\n|\n"+
		`meta{urn:shop}[n="m"](s="urn:shop" xsi="urn:xsi")`+"\n"+
		`item{urn:shop}[id="3"](s="urn:shop" xsi="urn:xsi")`+"\n|", outputs[0])
}

func TestRecordsResolveDetachedQName(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local r = check(xml.reader('<s:c xmlns:s="urn:s" xmlns:xsi="urn:xsi"><s:i xsi:type="s:Order"/></s:c>'))
		local records = check(r:select("*/*"))
		local batch = check(records:read())
		local rec = batch[1]
		return xml.resolve(rec, xml.attr(rec, "type", "urn:xsi"))`)
	assert.Equal(t, "Order", vals[0].String())
	assert.Equal(t, "urn:s", vals[1].String())
}

func TestRecordsErrors(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local r = check(xml.reader("<r><i/><i></r>"))
		local records = check(r:select("r/i"))
		local first, err = records:read(1)
		check(err == nil and #first == 1, "the first record precedes the error")
		local _, e1 = records:read(1)
		local _, e2 = records:read(1)
		local fresh = check(check(xml.reader("<r><i/></r>")):select("r/i"))
		local _, nerr = fresh:read(0)
		local _, ferr = fresh:read(1.5)
		local r2 = check(xml.reader("<a/>"))
		r2:next()
		local _, serr = r2:select("a")
		local r3 = check(xml.reader("<a/>"))
		local _, perr = r3:select("q:a")
		local _, aerr = r3:select({})
		local _, berr = r3:select("a", {ns = 1})
		return code(e1), code(e2), e1:message() == e2:message(), code(nerr), code(ferr), code(serr), code(perr), code(aerr), code(berr)`)
	got := make([]string, len(vals))
	for i, v := range vals {
		got[i] = v.String()
	}
	assert.Equal(t, []string{"syntax", "syntax", "true", "argument", "argument", "state", "namespace", "argument", "argument"}, got)
}

func TestReaderSourceErrors(t *testing.T) {
	e := newEnv(t)
	writeOnly, _ := e.sink(io.Discard)
	e.global("dest", writeOnly)
	vals := e.run(`local _, err = xml.reader(dest)
		return code(err)`)
	assert.Equal(t, "argument", vals[0].String())

	e.global("src", e.stream(io.MultiReader(strings.NewReader("<a>"), failing{err: errors.New("connection reset")})))
	vals = e.run(`local r = check(xml.reader(src))
		local kind, err
		repeat kind, err = r:next() until err or not kind
		return code(err), err:kind()`)
	assert.Equal(t, "io", vals[0].String())
	assert.Equal(t, "Internal", vals[1].String())
}

type failing struct{ err error }

func (f failing) Read([]byte) (int, error) { return 0, f.err }

// cancelAfter cancels a context once its reads exceed a count.
type cancelAfter struct {
	r      io.Reader
	cancel context.CancelFunc
	reads  int
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	c.reads--
	if c.reads < 0 {
		c.cancel()
	}
	if len(p) > 512 {
		p = p[:512]
	}
	return c.r.Read(p)
}

func TestCancellationMidRead(t *testing.T) {
	for _, script := range map[string]string{
		"next": `local kind, err
			repeat kind, err = r:next() until err or not kind
			return code(err), err:kind()`,
		"records": `local records = check(r:select("r/i"))
			local batch, err
			repeat batch, err = records:read(1000) until err or not batch
			return code(err), err:kind()`,
	} {
		e := newEnv(t)
		cmdCtx, cancel := context.WithCancel(e.ctx)
		e.cmdCtx = cmdCtx
		e.global("src", e.stream(&cancelAfter{r: strings.NewReader("<r>" + strings.Repeat(`<i v="x"/>`, 200000) + "</r>"), cancel: cancel, reads: 20}))
		vals := e.run(`local r = check(xml.reader(src))
			` + script)
		assert.Equal(t, "canceled", vals[0].String())
		assert.Equal(t, "Canceled", vals[1].String())
		cancel()
	}
}

func TestStreamReaderYieldsOnlyWhenBufferIsExhausted(t *testing.T) {
	e := newEnv(t)
	doc := "<r>" + strings.Repeat(`<i v="x"/>`, 50000) + "</r>"
	e.global("src", e.stream(strings.NewReader(doc)))
	vals := e.run(`local r = check(xml.reader(src))
		local n = 0
		while r:next() do n = n + 1 end
		return n`)
	assert.Equal(t, "100002", vals[0].String())
	fills := e.yields[xmlapi.ReaderFill]
	assert.Greater(t, fills, 1)
	assert.Less(t, fills, 200, "events arrive in batches")
}
