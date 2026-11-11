// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	xmlapi "github.com/wippyai/runtime/api/xml"
)

const dagLua = `
local t = {name = "x"}
for _ = 1, 40 do t = {name = "x", children = {t, t}} end
`

func TestSharedSubtreesStopAtNodeBytes(t *testing.T) {
	e := newEnv(t)
	sink, _ := e.sink(io.Discard)
	e.global("dest", sink)
	vals := e.run(dagLua + `
		local out, err = xml.encode(t)
		check(out == nil, "no output")
		local w = check(xml.writer())
		local ok, werr = w:node(t)
		check(ok == false, "node fails")
		local s = check(xml.writer(dest))
		local sok, serr = s:node(t)
		check(sok == false, "stream node fails")
		return code(err), err:details().limit, code(werr), werr:details().limit, serr:details().limit`)
	assert.Equal(t, []string{"limit", "max_node_bytes", "limit", "max_node_bytes", "max_node_bytes"},
		[]string{vals[0].String(), vals[1].String(), vals[2].String(), vals[3].String(), vals[4].String()})
}

func TestWriterLimitOptions(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`
		local tree = {name = "a", children = {{name = "b", text = "0123456789"}}}
		local out = check(xml.encode(tree))
		local fits = xml.encode(tree, {max_node_bytes = #out})
		local _, over = xml.encode(tree, {max_node_bytes = #out - 1})
		local _, depth = xml.encode(tree, {max_depth = 1})
		local _, attrs = xml.encode({name = "a", attrs = {x = "1", y = "2"}}, {max_attrs = 1})
		local _, ns = xml.encode({name = "a", xmlns = {p = "1", q = "2"}}, {max_namespaces = 1})
		local w = check(xml.writer(nil, {max_node_bytes = 17}))
		check(w:start_element("root"))
		for _ = 1, 4 do check(w:element("item", nil, "0123")) end
		local _, element = w:element("item", nil, string.rep("x", 20))
		local results = {fits == out, over:details().limit, depth:details().limit, attrs:details().limit, ns:details().limit, element:details().limit}
		for _, bad in ipairs({{max_node_bytes = 0}, {max_depth = -1}, {max_attrs = 1.5}, {max_namespaces = "1"}}) do
			local _, err = xml.encode(tree, bad)
			results[#results + 1] = code(err)
		end
		return table.unpack(results)`)
	got := make([]string, len(vals))
	for i, v := range vals {
		got[i] = v.String()
	}
	assert.Equal(t, []string{"true", "max_node_bytes", "max_depth", "max_attrs", "max_namespaces", "max_node_bytes",
		"argument", "argument", "argument", "argument"}, got)
}

func TestWriterFailuresAreSticky(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local w = check(xml.writer())
		check(w:start_element("a"))
		local _, first = w:text("bad \0 char")
		local ok, flush = w:flush()
		local doc, fin = w:finish()
		local _, later = w:end_element()
		return code(first), ok, flush and flush:message() == first:message(), doc, fin and fin:message() == first:message(), later and later:message() == first:message()`)
	assert.Equal(t, []string{"element", "false", "true", "nil", "true", "true"},
		[]string{vals[0].String(), vals[1].String(), vals[2].String(), vals[3].String(), vals[4].String(), vals[5].String()})

	dst := &countingWriter{fail: errors.New("disk full")}
	sink, _ := e.sink(dst)
	e.global("dest", sink)
	vals = e.run(`local w = check(xml.writer(dest))
		check(w:element("a"))
		local _, first = w:finish()
		local _, again = w:finish()
		local ok, flush = w:flush()
		return code(first), again and again:message() == first:message(), code(again), ok, flush and flush:message() == first:message()`)
	assert.Equal(t, []string{"io", "true", "io", "false", "true"},
		[]string{vals[0].String(), vals[1].String(), vals[2].String(), vals[3].String(), vals[4].String()})

	vals = e.run(`local w = check(xml.writer())
		check(w:element("a"))
		check(w:finish())
		local _, again = w:finish()
		local _, third = w:finish()
		return code(again), third:message() == again:message()`)
	assert.Equal(t, "state", vals[0].String())
	assert.Equal(t, "true", vals[1].String())
}

// TestConcurrentDropDuringVMWork drops handles from another goroutine, as frame
// teardown does, while the VM keeps writing and reading synchronously.
func TestConcurrentDropDuringVMWork(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	e.global("drop_later", lua.LGoFunc(func(l *lua.LState) int {
		var dropper interface{ Drop() }
		switch v := l.CheckUserData(1).Value.(type) {
		case *Writer:
			dropper = v.entry
		case *Reader:
			dropper = v.entry
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
			dropper.Drop()
		}()
		return 0
	}))
	doc := "<r>" + strings.Repeat(`<i v="x">text</i>`, 20000) + "</r>"
	e.global("doc", lua.LString(doc))
	for round := range 20 {
		vals := e.run(`
			local w = check(xml.writer())
			check(w:start_element("root"))
			drop_later(w)
			local werr
			for i = 1, 1000000 do
				local ok, err = w:element("item", {n = "1"}, "value")
				if not ok then werr = err break end
				if i % 1000 == 0 then
					local _, ferr = w:flush()
					if ferr then werr = ferr break end
				end
			end
			local r = check(xml.reader(doc))
			drop_later(r)
			local rerr
			while true do
				local kind, err = r:next()
				if err then rerr = err break end
				if not kind then break end
				if kind == "start" and r:depth() == 2 then
					local _, nerr = r:node()
					if nerr then rerr = nerr break end
				end
			end
			return code(werr), rerr and code(rerr) or "done"`)
		wg.Wait()
		assert.Equal(t, "state", vals[0].String(), "round %d", round)
		assert.Contains(t, []string{"state", "done"}, vals[1].String(), "round %d", round)
	}
}

func longNames(count, size int) string {
	var b strings.Builder
	b.WriteString("<r>")
	for i := range count {
		name := fmt.Sprintf("n%06d%s", i, strings.Repeat("x", size))
		b.WriteString("<" + name + "/>")
	}
	b.WriteString("</r>")
	return b.String()
}

func retainedHeap() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestInternerRetentionIsBounded(t *testing.T) {
	doc := longNames(4000, 1100)
	for name, script := range map[string]string{
		"cursor": `local r = check(xml.reader(src))
			while true do
				local kind = check_next(r)
				if not kind then break end
				local n = r:name()
			end
			kept = r`,
		"records": `local r = check(xml.reader(src))
			local records = check(r:select("r/*"))
			while true do
				local batch, err = records:read(50)
				check(err == nil, tostring(err))
				if not batch then break end
			end
			kept = records`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			require.NoError(t, e.l.DoString(`function check_next(r) local k, err = r:next() check(err == nil, tostring(err)) return k end`))
			e.global("src", e.stream(strings.NewReader(doc)))
			base := retainedHeap()
			e.run(script)
			growth := int64(retainedHeap()) - int64(base)
			t.Logf("%s: retained growth %d KiB with the handle open", name, growth>>10)
			assert.Less(t, growth, int64(2<<20), "4 MB of distinct names must not stay pinned")
		})
	}
}

func TestInternerBudget(t *testing.T) {
	in := newInterner()
	for i := range 20000 {
		in.name(fmt.Sprintf("%06d%s", i, strings.Repeat("n", 900)))
		in.value([]byte(fmt.Sprintf("%06d", i)))
	}
	assert.LessOrEqual(t, in.bytes, maxInternedTotal)
	long := strings.Repeat("y", maxInternedName+1)
	assert.Equal(t, long, in.name(long).String())
	_, kept := in.values[long]
	assert.False(t, kept, "names above the cutoff are not retained")
}

func TestTerminalErrorsPrecedeArgumentChecks(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local w = check(xml.writer())
		check(w:start_element("a"))
		local _, first = w:text("bad \0 char")
		local same = {}
		local calls = {
			function() return w:node(false) end,
			function() return w:start_element(1) end,
			function() return w:element(nil) end,
			function() return w:text(1) end,
			function() return w:comment({}) end,
			function() return w:end_element() end,
			function() return w:flush() end,
			function() return w:finish() end,
		}
		for i, call in ipairs(calls) do
			local ok, res, err = pcall(call)
			check(ok, "method " .. i .. " raised " .. tostring(res))
			same[#same + 1] = tostring(err ~= nil and err:message() == first:message() and code(err) == "element")
		end
		return table.concat(same, ",")`)
	assert.Equal(t, "true,true,true,true,true,true,true,true", vals[0].String())
}

func TestReaderFailuresAreTerminal(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local out = {}
		local r = check(xml.reader("<r><a>12345</a><b/></r>", {max_node_bytes = 4}))
		r:next(); r:next()
		local _, first = r:node()
		local w = check(xml.writer())
		local results = {select(2, r:node()), select(2, r:skip()), select(2, r:copy(w)), select(2, r:next())}
		for _, err in ipairs(results) do
			out[#out + 1] = tostring(err ~= nil and err:message() == first:message() and code(err) == "limit")
		end
		local s = check(xml.reader("<r><a></b></r>"))
		local _, syntax
		repeat local k, err = s:next(); syntax = err until err or not k
		local after = {select(2, s:node()), select(2, s:skip()), select(2, s:copy(w)), select(2, s:next()), select(2, s:select("r/a"))}
		for _, err in ipairs(after) do
			out[#out + 1] = tostring(err ~= nil and err:message() == syntax:message() and code(err) == "syntax")
		end
		local failed = check(xml.writer())
		check(failed:start_element("x"))
		local _, werr = failed:text("\0")
		local c = check(xml.reader("<a/>"))
		c:next()
		local _, cerr = c:copy(failed)
		out[#out + 1] = tostring(cerr ~= nil and cerr:message() == werr:message())
		return table.concat(out, ",")`)
	assert.Equal(t, "true,true,true,true,true,true,true,true,true,true", vals[0].String())
}

// retainedBytes sums the distinct string data an interner references.
func retainedBytes(in *interner) int {
	seen := map[*byte]bool{}
	total := 0
	add := func(s string) {
		if s == "" {
			return
		}
		p := unsafe.StringData(s)
		if !seen[p] {
			seen[p] = true
			total += len(s)
		}
	}
	for k, v := range in.values {
		add(k)
		add(string(v.(lua.LString)))
	}
	for _, slot := range in.recent {
		add(slot.s)
		if slot.v != nil {
			add(string(slot.v.(lua.LString)))
		}
	}
	for _, slot := range in.keys {
		add(slot.key)
	}
	return total
}

func TestInternerRetentionIsExact(t *testing.T) {
	in := newInterner()
	for i := 0; in.bytes+1000 <= maxInternedTotal; i++ {
		in.name(fmt.Sprintf("fill%06d%s", i, strings.Repeat("f", 990)))
	}
	for i := range 20000 {
		in.name(fmt.Sprintf("late%06d%s", i, strings.Repeat("n", 1000)))
		in.value([]byte(fmt.Sprintf("v%06d%s", i, strings.Repeat("v", 50))))
		ns := fmt.Sprintf("urn:%06d:%s", i, strings.Repeat("u", 900))
		in.attrKey(ns, fmt.Sprintf("k%06d", i))
	}
	assert.LessOrEqual(t, retainedBytes(in), maxInternedTotal, "slots reference only accounted strings")
	assert.LessOrEqual(t, in.bytes, maxInternedTotal)

	key := in.attrKey(strings.Repeat("u", maxInternedName-6), "abcd")
	assert.Len(t, key, maxInternedName, "a key of exactly the cutoff, braces included, is cacheable")
	fresh := newInterner()
	fresh.attrKey(strings.Repeat("u", maxInternedName-5), "abcd")
	assert.Zero(t, retainedBytes(fresh), "a key one byte past the cutoff is not kept")
}

func TestHelpersAreBudgeted(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`
		local function dag(levels)
			local t = {name = "x", text = "x"}
			for _ = 1, levels do t = {name = "x", children = {t, t}} end
			return t
		end
		local out = {}
		local deep_ok, deep_err = pcall(xml.text, dag(40))
		out[#out + 1] = tostring(not deep_ok and deep_err:details().limit == "max_node_bytes")
		local deep_found, deep_ferr = xml.find(dag(40), string.rep("*/", 39) .. "*")
		out[#out + 1] = tostring(deep_found == nil and deep_ferr:details().limit == "max_node_bytes")
		local ok, err = pcall(xml.text, dag(24))
		out[#out + 1] = tostring(not ok and code(err) == "limit" and err:details().limit == "max_node_bytes" and err:kind() == errors.INVALID)
		local path = string.rep("*/", 23) .. "*"
		local found, ferr = xml.find(dag(24), path)
		out[#out + 1] = tostring(found == nil and code(ferr) == "limit" and ferr:details().limit == "max_node_bytes")
		local cyclic = {name = "c"}
		cyclic.children = {cyclic}
		local cok, cerr = pcall(xml.text, cyclic)
		out[#out + 1] = tostring(not cok and code(cerr) == "limit")
		local cfound, cferr = xml.find(cyclic, string.rep("*/", 299) .. "*")
		out[#out + 1] = tostring(cfound == nil and code(cferr) == "limit")
		local small = dag(10)
		out[#out + 1] = tostring(#xml.text(small) == 1024)
		local raised = xml.text(dag(16), {max_node_bytes = 64 * 1024 * 1024})
		out[#out + 1] = tostring(#raised == 65536)
		local matches, merr = xml.find(dag(16), string.rep("*/", 15) .. "*", {max_node_bytes = 64 * 1024 * 1024})
		out[#out + 1] = tostring(merr == nil and #matches == 65536)
		local ns = xml.find({name = "r", children = {{name = "i", ns = "urn:s"}}}, "s:i", {ns = {s = "urn:s"}})
		out[#out + 1] = tostring(#ns == 1)
		return table.concat(out, ",")`)
	assert.Equal(t, "true,true,true,true,true,true,true,true,true,true", vals[0].String())
}

func prefixedDocument(prefixBytes, n int) string {
	prefix := "p" + strings.Repeat("x", prefixBytes-1)
	var b strings.Builder
	b.WriteString(`<r xmlns:` + prefix + `="urn:x">`)
	for i := range n {
		fmt.Fprintf(&b, `<%s:n%06d k="v"/>`, prefix, i)
	}
	b.WriteString(`</r>`)
	return b.String()
}

func TestShortNamesDoNotPinLongPrefixesInLua(t *testing.T) {
	doc := prefixedDocument(64<<10, 500)
	for name, script := range map[string]string{
		"name": `local r = check(xml.reader(src))
			kept_names = {}
			while true do
				local kind, err = r:next()
				check(err == nil, tostring(err))
				if not kind then break end
				if kind == "start" then kept_names[#kept_names + 1] = r:name() end
			end
			kept = r`,
		"records": `local r = check(xml.reader(src))
			local records = check(r:select("r/*"))
			kept_names = {}
			while true do
				local batch, err = records:read(1000)
				check(err == nil, tostring(err))
				if not batch then break end
				for _, rec in ipairs(batch) do kept_names[#kept_names + 1] = rec.name end
			end
			kept = records`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.global("src", e.stream(strings.NewReader(doc)))
			base := retainedHeap()
			e.run(script)
			growth := int64(retainedHeap()) - int64(base)
			t.Logf("%s: retained growth %d KiB holding 500 names and the handle", name, growth>>10)
			assert.Less(t, growth, int64(4<<20))
		})
	}
}

func TestFailurePrecedesClosedAndArguments(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local out = {}
		local function same(err, first, want)
			return tostring(err ~= nil and err:message() == first:message() and code(err) == want)
		end
		local r = check(xml.reader("<r><a>12345</a></r>", {max_node_bytes = 4}))
		r:next(); r:next()
		local _, first = r:node()
		local ok, res, err = pcall(function() return r:copy(nil) end)
		out[#out + 1] = tostring(ok) .. ":" .. same(err, first, "limit")
		r:close()
		out[#out + 1] = same(select(2, r:next()), first, "limit")
		out[#out + 1] = same(select(2, r:node()), first, "limit")
		out[#out + 1] = same(select(2, r:select("r/a")), first, "limit")

		local s = check(xml.reader("<r><i/><i></r>"))
		local records = check(s:select("r/i"))
		records:read(1)
		local _, rfirst = records:read(1)
		records:close()
		out[#out + 1] = same(select(2, records:read(1)), rfirst, "syntax")
		local okr, rres, rerr = pcall(function() return records:read("x") end)
		out[#out + 1] = tostring(okr) .. ":" .. same(rerr, rfirst, "syntax")

		local w = check(xml.writer())
		check(w:start_element("a"))
		local _, wfirst = w:text("\0")
		w:close()
		out[#out + 1] = same(select(2, w:text("x")), wfirst, "element")
		out[#out + 1] = same(select(2, w:finish()), wfirst, "element")
		return table.concat(out, ",")`)
	assert.Equal(t, "true:true,true,true,true,true,true:true,true,true", vals[0].String())
}

func TestHelperBudgetBoundaries(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local out = {}
		local function text_at(tree, opts)
			local ok, err = pcall(xml.text, tree, opts)
			if ok then return "ok" end
			return err:details().limit
		end
		local function find_at(tree, path, opts)
			local found, err = xml.find(tree, path, opts)
			if found then return "ok" end
			return err:details().limit
		end
		local leaf = {name = "r", text = "abc"}
		local mixed = {name = "r", children = {"ab", {name = "x", text = "c"}}}
		local pair = {name = "r", children = {{name = "x"}}}
		local deep = {name = "r", children = {{name = "a", children = {{name = "b"}}}}}
		for _, case in ipairs({
			{function(n) return text_at(leaf, {max_node_bytes = n}) end, 7},
			{function(n) return text_at(mixed, {max_node_bytes = n}) end, 11},
			{function(n) return find_at(pair, "*", {max_node_bytes = n}) end, 8},
			{function(n) return find_at(deep, "a/b", {max_node_bytes = n}) end, 12},
			{function(n) return text_at(deep, {max_depth = n}) end, 3},
			{function(n) return find_at(deep, "a/b", {max_depth = n}) end, 3},
		}) do
			out[#out + 1] = case[1](case[2] - 1) .. "/" .. case[1](case[2]) .. "/" .. case[1](case[2] + 1)
		end
		return table.concat(out, ",")`)
	assert.Equal(t, "max_node_bytes/ok/ok,max_node_bytes/ok/ok,max_node_bytes/ok/ok,max_node_bytes/ok/ok,max_depth/ok/ok,max_depth/ok/ok", vals[0].String())
}

func TestWriterSiblingsStayFlat(t *testing.T) {
	e := newEnv(t)
	sink, _ := e.sink(io.Discard)
	e.global("dest", sink)
	e.run(`w = check(xml.writer(dest))
		check(w:start_element("root", nil, "urn:u"))
		child = {name = "c", ns = "urn:u", xmlns = {p = "urn:u"}, attrs = {["{urn:u}k"] = "v"}}
		shadow = {name = "d", xmlns = {[""] = "urn:v", p = "urn:u"}}
		function siblings(n)
			for i = 1, n do
				check(w:node(i % 2 == 0 and child or shadow))
				check(w:start_element("e", nil, "urn:u"))
				check(w:end_element())
			end
		end
		siblings(10000)`)
	base := retainedHeap()
	e.run(`siblings(990000)`)
	growth := int64(retainedHeap()) - int64(base)
	t.Logf("live growth after one million siblings: %d KiB", growth>>10)
	assert.Less(t, growth, int64(512<<10))
}

// siblingStream produces siblings with distinct names and values without holding the document.
type siblingStream struct {
	pending []byte
	n, i    int
}

func (s *siblingStream) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		switch {
		case s.i == 0:
			s.pending = []byte(`<r xmlns:p="urn:p">`)
		case s.i <= s.n:
			s.pending = []byte(fmt.Sprintf(`<n%07d%s p:k="%07d" xmlns:q="urn:%07d"/>`, s.i, strings.Repeat("x", 400), s.i, s.i))
		case s.i == s.n+1:
			s.pending = []byte(`</r>`)
		default:
			return 0, io.EOF
		}
		s.i++
	}
	k := copy(p, s.pending)
	s.pending = s.pending[k:]
	return k, nil
}

func TestLuaCursorStaysFlatOverDistinctSiblings(t *testing.T) {
	e := newEnv(t)
	e.global("src", e.stream(&siblingStream{n: 200_000}))
	e.run(`r = check(xml.reader(src))
		function walk(n)
			local seen = 0
			while seen < n do
				local kind, err = r:next()
				check(err == nil, tostring(err))
				if not kind then return end
				if kind == "start" then
					seen = seen + 1
					local name, value, attrs = r:name(), r:attr("k", "urn:p"), r:attrs()
				end
			end
		end
		walk(20000)`)
	base := retainedHeap()
	e.run(`walk(200000)`)
	growth := int64(retainedHeap()) - int64(base)
	t.Logf("growth from 20k to 200k distinct siblings: %d KiB", growth>>10)
	assert.Less(t, growth, int64(1<<20))
}

func TestLuaWriterRetainsNoKeySubstrings(t *testing.T) {
	e := newEnv(t)
	sink, _ := e.sink(io.Discard)
	e.global("dest", sink)
	e.run(`w = check(xml.writer(dest, {max_tag_bytes = 1073741824}))
		check(w:start_element("root"))
		check(w:flush())`)
	base := retainedHeap()
	e.run(`for i = 1, 200 do
			local key = "{urn:" .. i .. ":" .. string.rep("u", 262144) .. "}k" .. i
			check(w:element("e" .. i, {[key] = "v"}))
			check(w:flush())
		end`)
	open := int64(retainedHeap()) - int64(base)
	t.Logf("retained with the writer open: %d KiB", open>>10)
	assert.Less(t, open, int64(1<<20))
	e.run(`w:close()`)
	closed := int64(retainedHeap()) - int64(base)
	assert.Less(t, closed, int64(256<<10))
}

func TestAccessorsOnFailedAndClosedReaders(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local function probe(r)
			local results = {}
			local function run(name, f)
				local ok, a = pcall(f)
				if not ok then
					results[#results + 1] = name .. "=raised"
				elseif type(a) == "table" then
					local n = 0
					for k in pairs(a) do n = n + 1 end
					results[#results + 1] = name .. "=" .. (a.line ~= nil and (a.line .. ":" .. a.column .. ":" .. a.offset) or ("table" .. n))
				else
					results[#results + 1] = name .. "=" .. tostring(a)
				end
			end
			run("kind", function() return r:kind() end)
			run("name", function() return r:name() end)
			run("ns", function() return r:ns() end)
			run("attr", function() return r:attr("x") end)
			run("attr_bad", function() return r:attr(false) end)
			run("attrs", function() return r:attrs() end)
			run("text", function() return r:text() end)
			run("depth", function() return r:depth() end)
			run("pos", function() return r:pos() end)
			return table.concat(results, ",")
		end
		local failed = check(xml.reader('<r xmlns="urn:r"><a x="1">text</b></r>'))
		local kind, err
		repeat kind, err = failed:next() until err or not kind
		local closed = check(xml.reader('<r xmlns="urn:r"><a x="1"/></r>'))
		closed:next(); closed:next()
		closed:close()
		local limited = check(xml.reader('<r><a x="1">0123456789</a></r>', {max_node_bytes = 8}))
		limited:next(); limited:next()
		local _, nerr = limited:node()
		check(code(nerr) == "limit", "node fails")
		return probe(failed), probe(closed), probe(limited)`)
	want := "kind=nil,name=nil,ns=nil,attr=nil,attr_bad=nil,attrs=table0,text=nil,depth=0,pos=0:0:0"
	assert.Equal(t, want, vals[0].String(), "failed reader")
	assert.Equal(t, want, vals[1].String(), "closed reader")
	assert.Equal(t, want, vals[2].String(), "reader failed in node")
}

func TestFailedCopyMakesTheReaderTerminal(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local r = check(xml.reader("<abcd/>"))
		check(r:next())
		local w = check(xml.writer(nil, {max_tag_bytes = 3}))
		local ok, first = r:copy(w)
		local _, after = r:next()
		local _, again = r:node()
		return tostring(ok), code(first), tostring(after ~= nil and after:message() == first:message()), tostring(again ~= nil and again:message() == first:message())`)
	assert.Equal(t, []string{"false", "limit", "true", "true"}, []string{vals[0].String(), vals[1].String(), vals[2].String(), vals[3].String()})

	dst := &countingWriter{fail: errors.New("disk full")}
	sink, _ := e.sink(dst)
	e.global("dest", sink)
	e.global("src", e.stream(strings.NewReader("<r><big>"+strings.Repeat("<x>payload</x>", 20000)+"</big><after/></r>")))
	vals = e.run(`local r = check(xml.reader(src))
		check(r:next()); check(r:next())
		local w = check(xml.writer(dest))
		local ok, first = r:copy(w)
		local _, after = r:next()
		return tostring(ok), code(first), tostring(after ~= nil and after:message() == first:message())`)
	assert.Equal(t, []string{"false", "io", "true"}, []string{vals[0].String(), vals[1].String(), vals[2].String()})
}

func TestLuaWriterScratchIsClearedOnEveryExit(t *testing.T) {
	e := newEnv(t)
	e.run(`w = check(xml.writer())`)
	base := retainedHeap()
	e.run(`local ok = pcall(function()
			w:start_element("a", {[string.rep("k", 4194304)] = "v"}, false)
		end)
		check(not ok, "a wrong namespace argument raises")`)
	failed := int64(retainedHeap()) - int64(base)
	t.Logf("retained after a raised start_element: %d KiB", failed>>10)
	assert.Less(t, failed, int64(512<<10))

	e.run(`w2 = check(xml.writer(nil, {max_tag_bytes = 16777216, max_node_bytes = 16777216}))
		check(w2:start_element("root"))
		check(w2:element("a", {[string.rep("k", 4194304)] = "v"}))
		w2:close()`)
	closed := int64(retainedHeap()) - int64(base)
	t.Logf("retained after close: %d KiB", closed>>10)
	assert.Less(t, closed, int64(512<<10))
}

func TestBufferedCopyFlushFailureMakesTheReaderTerminal(t *testing.T) {
	e := newEnv(t)
	dst := &countingWriter{fail: errors.New("disk full")}
	sink, _ := e.sink(dst)
	e.global("dest", sink)
	e.global("doc", lua.LString("<r><a>"+strings.Repeat("&amp;", 20000)+"</a><b/></r>"))
	vals := e.run(`local r = check(xml.reader(doc))
		check(r:next()); check(r:next())
		local w = check(xml.writer(dest))
		local ok, first = r:copy(w)
		local _, after = r:next()
		local _, node = r:node()
		return tostring(ok), code(first), tostring(after ~= nil and after:message() == first:message()), tostring(node ~= nil and node:message() == first:message())`)
	assert.Equal(t, []string{"false", "io", "true", "true"}, []string{vals[0].String(), vals[1].String(), vals[2].String(), vals[3].String()})
	assert.Equal(t, 1, e.yields[xmlapi.WriterFlush], "the element was copied from the buffer and flushed once")
}

func TestLuaWriterScratchIsClearedWhenAttrsRaise(t *testing.T) {
	e := newEnv(t)
	e.run(`w = check(xml.writer(nil, {max_tag_bytes = 16777216, max_node_bytes = 16777216}))
		check(w:start_element("root"))
		check(w:element("a", {x = "1"}))
		check(w:element("b"))`)
	base := retainedHeap()
	e.run(`local ok = pcall(function()
			w:start_element("c", {big = string.rep("p", 4194304), bad = 1})
		end)
		check(not ok, "a non-string attribute value raises")`)
	retained := int64(retainedHeap()) - int64(base)
	t.Logf("retained after attrs raised while collecting: %d KiB", retained>>10)
	assert.Less(t, retained, int64(512<<10))
}

func TestLuaWriterReleasesOversizedScratch(t *testing.T) {
	e := newEnv(t)
	e.run(`w = check(xml.writer())
		check(w:start_element("root"))
		local attrs = {}
		for i = 1, 200000 do attrs["a" .. i] = "v" end
		local ok, err = w:start_element("x", attrs)
		check(not ok and err:details().limit == "max_attrs", "too many attributes is a limit error")`)
	ud, ok := e.l.GetGlobal("w").(*lua.LUserData)
	require.True(t, ok)
	w, ok := ud.Value.(*Writer)
	require.True(t, ok)
	t.Logf("scratch capacity kept after a rejected 200000-attribute table: %d", cap(w.attrs))
	assert.LessOrEqual(t, cap(w.attrs), keptScratch)
}
