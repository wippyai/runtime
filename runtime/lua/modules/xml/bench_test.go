// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
)

const benchRecords = 60000

// benchDocuments returns an XML record feed of about 10 MB and the equivalent JSON document.
func benchDocuments() (string, string) {
	var x, j strings.Builder
	x.WriteString(`<feed xmlns="urn:feed" xmlns:x="urn:x">`)
	j.WriteString(`{"items":[`)
	for i := 1; i <= benchRecords; i++ {
		id := strconv.Itoa(i)
		x.WriteString(`<item id="` + id + `" x:status="active"><name>Item ` + id + `</name>` +
			`<price currency="USD">12.50</price><description>A product description for item ` + id +
			`</description><tags><tag>alpha</tag><tag>beta</tag></tags></item>`)
		if i > 1 {
			j.WriteByte(',')
		}
		j.WriteString(`{"id":"` + id + `","status":"active","name":"Item ` + id + `","price":{"currency":"USD","value":"12.50"},` +
			`"description":"A product description for item ` + id + `","tags":["alpha","beta"]}`)
	}
	x.WriteString(`</feed>`)
	j.WriteString(`]}`)
	return x.String(), j.String()
}

var benchXML, benchJSON = benchDocuments()

func benchEnv(b *testing.B) *env {
	e := newEnv(b)
	mod, _ := jsonmod.Module.Build()
	e.global("json", mod)
	return e
}

func runBench(b *testing.B, e *env, size int, setup func(), script string) {
	fn, err := e.l.LoadString(script)
	if err != nil {
		b.Fatal(err)
	}
	e.global("work", fn)
	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		setup()
		b.StartTimer()
		e.run(`return work()`)
	}
}

func BenchmarkXMLDecode(b *testing.B) {
	e := benchEnv(b)
	e.global("doc", lua.LString(benchXML))
	runBench(b, e, len(benchXML), func() {}, `local t, err = xml.decode(doc, {max_node_bytes = 64 * 1024 * 1024})
		if err then error(err) end`)
}

func BenchmarkJSONDecode(b *testing.B) {
	e := benchEnv(b)
	e.global("doc", lua.LString(benchJSON))
	runBench(b, e, len(benchJSON), func() {}, `local t, err = json.decode(doc)
		if err then error(err) end`)
}

func BenchmarkXMLEncode(b *testing.B) {
	e := benchEnv(b)
	e.global("doc", lua.LString(benchXML))
	e.run(`tree = check(xml.decode(doc, {max_node_bytes = 64 * 1024 * 1024}))`)
	runBench(b, e, len(benchXML), func() {}, `local s, err = xml.encode(tree, {max_node_bytes = 64 * 1024 * 1024})
		if err then error(err) end`)
}

func BenchmarkJSONEncode(b *testing.B) {
	e := benchEnv(b)
	e.global("doc", lua.LString(benchJSON))
	e.run(`tree = check(json.decode(doc))`)
	runBench(b, e, len(benchJSON), func() {}, `local s, err = json.encode(tree)
		if err then error(err) end`)
}

func BenchmarkXMLRecordsStream(b *testing.B) {
	e := benchEnv(b)
	runBench(b, e, len(benchXML), func() { e.global("src", e.stream(strings.NewReader(benchXML))) },
		`local r = check(xml.reader(src))
		local records = check(r:select("*:feed/*:item"))
		local n = 0
		while true do
			local batch, err = records:read(100)
			if err then error(err) end
			if not batch then break end
			n = n + #batch
		end
		records:close()
		if n ~= `+strconv.Itoa(benchRecords)+` then error("records " .. n) end`)
}

func BenchmarkXMLCursorStream(b *testing.B) {
	e := benchEnv(b)
	runBench(b, e, len(benchXML), func() { e.global("src", e.stream(strings.NewReader(benchXML))) },
		`local r = check(xml.reader(src))
		local starts = 0
		while true do
			local kind, err = r:next()
			if err then error(err) end
			if not kind then break end
			if kind == "start" then starts = starts + 1 end
		end
		r:close()`)
}

func BenchmarkXMLCopyStream(b *testing.B) {
	e := benchEnv(b)
	runBench(b, e, len(benchXML), func() {
		e.global("src", e.stream(strings.NewReader(benchXML)))
		sink, _ := e.sink(io.Discard)
		e.global("dest", sink)
	}, `local r = check(xml.reader(src))
		local w = check(xml.writer(dest))
		r:next()
		check(r:copy(w))
		local _, err = w:finish()
		if err then error(err) end
		r:close()
		w:close()`)
}

func TestBenchDocumentsAreEquivalent(t *testing.T) {
	e := newEnv(t)
	mod, _ := jsonmod.Module.Build()
	e.global("json", mod)
	e.global("xdoc", lua.LString(benchXML))
	e.global("jdoc", lua.LString(benchJSON))
	vals := e.run(`local x = check(xml.decode(xdoc, {max_node_bytes = 64 * 1024 * 1024}))
		local j = check(json.decode(jdoc))
		eq(#x.children, #j.items, "record count")
		local first = x.children[1]
		eq(first.attrs.id, j.items[1].id)
		eq(xml.text(xml.child(first, "description")), j.items[1].description)
		return #j.items`)
	if vals[0].String() != strconv.Itoa(benchRecords) {
		t.Fatalf("records %s", vals[0].String())
	}
	if !bytes.Contains([]byte(benchXML), []byte(`id="60000"`)) {
		t.Fatal("feed is incomplete")
	}
}
