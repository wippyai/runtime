// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	xmlapi "github.com/wippyai/runtime/api/xml"
)

func TestModuleDefinition(t *testing.T) {
	assert.Equal(t, "xml", Module.Name)
	assert.Equal(t, []string{luaapi.ClassEncoding, luaapi.ClassDeterministic}, Module.Class)
	mod, yields := Module.Build()
	assert.True(t, mod.Immutable)
	for _, name := range []string{"decode", "encode", "text", "attr", "child", "children", "find", "resolve", "reader", "writer"} {
		assert.Equal(t, lua.LTFunction, mod.RawGetString(name).Type(), name)
	}
	ids := map[dispatcher.CommandID]bool{}
	for _, y := range yields {
		ids[y.CmdID] = true
		conv, ok := y.Sample.(luaapi.YieldConverter)
		require.True(t, ok)
		assert.Equal(t, y.CmdID, conv.CmdID())
		_, ok = y.Sample.(luaapi.HandledYield)
		assert.True(t, ok)
	}
	assert.Equal(t, map[dispatcher.CommandID]bool{
		xmlapi.ReaderFill: true, xmlapi.ReaderNode: true, xmlapi.ReaderSkip: true,
		xmlapi.ReaderCopy: true, xmlapi.RecordsRead: true, xmlapi.WriterFlush: true,
	}, ids)
}

func TestDecodeNormalForm(t *testing.T) {
	cases := []struct{ name, src, opts, want string }{
		{"empty forms are equal", `<a></a>`, "", `a`},
		{"self closing", `<a/>`, "", `a`},
		{"text only", `<a>x</a>`, "", `a"x"`},
		{"whitespace preserved", "<a> <b/> </a>", "", `a<" ",b," ">`},
		{"whitespace stripped", "<a> <b/> </a>", `{whitespace = "strip"}`, `a<b>`},
		{"strip keeps leaf text", "<a> x </a>", `{whitespace = "strip"}`, `a" x "`},
		{"strip empties whitespace leaf", "<a>  </a>", `{whitespace = "strip"}`, `a`},
		{"xml:space preserve", `<a xml:space="preserve"> <b/></a>`, `{whitespace = "strip"}`, `a[{http://www.w3.org/XML/1998/namespace}space="preserve"]<" ",b>`},
		{"CDATA joins text", "<a>x<![CDATA[<y>]]>z</a>", "", `a"x<y>z"`},
		{"comments and instructions never enter a tree", "<a>x<!--c-->y<?p d?></a>", `{comments = true, instructions = true}`, `a"xy"`},
		{"mixed content order", "<p>a<b>B</b>c<i/>d</p>", "", `p<"a",b"B","c",i,"d">`},
		{"entities", "<a>&lt;&gt;&amp;&apos;&quot;&#65;&#x42;</a>", "", `a"<>&'\"AB"`},
		{"line endings", "<a>1\r\n2\r3</a>", "", `a"1\n2\n3"`},
		{"attribute normalisation", "<a x=\"1\t2\n3\"/>", "", `a[x="1 2 3"]`},
		{"values are strings", `<a n="1" b="true">2</a>`, "", `a[b="true" n="1"]"2"`},
		{"default namespace", `<a xmlns="u"><b/></a>`, "", `a{u}(="u")<b{u}>`},
		{"unprefixed attribute has no namespace", `<a xmlns="u" x="1"/>`, "", `a{u}[x="1"](="u")`},
		{"qualified attribute key", `<a xmlns:p="v" p:x="1" y="2"/>`, "", `a[y="2" {v}x="1"](p="v")`},
		{"undeclared default", `<a xmlns="u"><b xmlns=""/></a>`, "", `a{u}(="u")<b(="")>`},
		{"descendants carry own declarations", `<a xmlns:p="u"><b xmlns:q="v"><p:c/></b></a>`, "", `a(p="u")<b(q="v")<c{u}>>`},
		{"xml prefix is never listed", `<a xml:lang="en"/>`, "", `a[{http://www.w3.org/XML/1998/namespace}lang="en"]`},
		{"misc around root", "<?xml version=\"1.0\"?><!--x-->\n<a/>\n<!--y-->", "", `a`},
		{"utf-16", "\xff\xfe<\x00a\x00/\x00>\x00", "", `a`},
	}
	e := newEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.global("src", lua.LString(tc.src))
			opts := tc.opts
			if opts == "" {
				opts = "nil"
			}
			vals := e.run(`local doc, err = xml.decode(src, ` + opts + `)
				check(err == nil, tostring(err))
				check(getmetatable(doc) == nil, "elements carry no metatable")
				return dump(doc)`)
			assert.Equal(t, tc.want, vals[0].String())
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct{ name, src, opts, code, limit string }{
		{"syntax", "<a>\n  <b></c>\n</a>", "", "syntax", ""},
		{"trailing content", "<a/><b/>", "", "syntax", ""},
		{"doctype", "<!DOCTYPE a><a/>", "", "syntax", ""},
		{"unknown entity", "<a>&nbsp;</a>", "", "syntax", ""},
		{"duplicate attribute", `<a x="1" x="2"/>`, "", "syntax", ""},
		{"duplicate expanded attribute", `<a xmlns:p="u" xmlns:q="u" p:x="1" q:x="2"/>`, "", "namespace", ""},
		{"unbound prefix", `<p:a/>`, "", "namespace", ""},
		{"malformed utf-8", "<a>\xff</a>", "", "encoding", ""},
		{"unsupported encoding", `<?xml version="1.0" encoding="EBCDIC"?><a/>`, "", "encoding", ""},
		{"max_depth", "<a><b><c/></b></a>", "{max_depth = 2}", "limit", "max_depth"},
		{"max_attrs", `<a x="1" y="2"/>`, "{max_attrs = 1}", "limit", "max_attrs"},
		{"max_namespaces", `<a xmlns:p="u"><b xmlns:q="v"/></a>`, "{max_namespaces = 1}", "limit", "max_namespaces"},
		{"max_tag_bytes", `<a xxxxxxxxxxxx="1"/>`, "{max_tag_bytes = 8}", "limit", "max_tag_bytes"},
		{"max_node_bytes", `<a>0123456789</a>`, "{max_node_bytes = 8}", "limit", "max_node_bytes"},
		{"max_bytes", `<a>0123456789</a>`, "{max_bytes = 8}", "limit", "max_bytes"},
	}
	e := newEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.global("src", lua.LString(tc.src))
			opts := tc.opts
			if opts == "" {
				opts = "nil"
			}
			vals := e.run(`local doc, err = xml.decode(src, ` + opts + `)
				check(doc == nil, "no tree on error")
				eq(err:kind(), errors.INVALID, "kind")
				eq(err:retryable(), false, "retryable")
				local d = err:details()
				check(type(d.line) == "number" and type(d.column) == "number" and type(d.offset) == "number", "position")
				check(type(d.path) == "table", "path")
				return d.code, d.limit or "", d.line, #d.path`)
			assert.Equal(t, tc.code, vals[0].String())
			assert.Equal(t, tc.limit, vals[1].String())
		})
	}
	vals := e.run(`local _, err = xml.decode("<a>\n  <b></c>\n</a>")
		local d = err:details()
		return d.line, d.path[1], d.path[2]`)
	assert.Equal(t, "2", vals[0].String())
	assert.Equal(t, "a", vals[1].String())
	assert.Equal(t, "b", vals[2].String())
}

func TestOptionValidation(t *testing.T) {
	e := newEnv(t)
	for _, opts := range []string{
		`{whitespace = "trim"}`, `{whitespace = 1}`, `{comments = "yes"}`, `{instructions = 1}`,
		`{max_depth = 0}`, `{max_depth = -1}`, `{max_depth = 1.5}`, `{max_depth = "8"}`,
		`{max_attrs = 0}`, `{max_namespaces = 0}`, `{max_tag_bytes = 0}`, `{max_token_bytes = 0}`,
		`{max_node_bytes = 0}`, `{max_bytes = -1}`, `{unknown = true}`, `{[1] = true}`, `"strip"`,
	} {
		t.Run(opts, func(t *testing.T) {
			vals := e.run(`local results = {}
				local _, err = xml.decode("<a/>", ` + opts + `)
				results[1] = code(err)
				local _, rerr = xml.reader("<a/>", ` + opts + `)
				results[2] = code(rerr)
				return results[1], results[2], err:kind()`)
			assert.Equal(t, "argument", vals[0].String())
			assert.Equal(t, "argument", vals[1].String())
			assert.Equal(t, "Invalid", vals[2].String())
		})
	}
	for _, opts := range []string{`{indent = 1}`, `{indent = "x"}`, `{declaration = 1}`, `{ns = {p = 1}}`, `{ns = "u"}`, `{nope = 1}`} {
		t.Run(opts, func(t *testing.T) {
			vals := e.run(`local _, err = xml.encode({name = "a"}, ` + opts + `)
				local _, werr = xml.writer(nil, ` + opts + `)
				return code(err), code(werr)`)
			assert.Equal(t, "argument", vals[0].String())
			assert.Equal(t, "argument", vals[1].String())
		})
	}
	vals := e.run(`local doc, err = xml.decode("<a>  <b/></a>", {whitespace = "strip", comments = true, instructions = false,
			max_depth = 4, max_attrs = 4, max_namespaces = 4, max_tag_bytes = 64, max_token_bytes = 64, max_node_bytes = 64, max_bytes = 0})
		check(err == nil, tostring(err))
		return dump(doc)`)
	assert.Equal(t, "a<b>", vals[0].String())
}

func TestArgumentErrors(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`
		local out = {}
		local _, err = xml.decode(1); out[#out + 1] = code(err)
		_, err = xml.encode("a"); out[#out + 1] = code(err)
		_, err = xml.reader(1); out[#out + 1] = code(err)
		_, err = xml.reader({}); out[#out + 1] = code(err)
		_, err = xml.writer("x"); out[#out + 1] = code(err)
		local _, _, rerr = xml.resolve("x", "a"); out[#out + 1] = code(rerr)
		for _, f in ipairs({function() xml.text(1) end, function() xml.attr({name = "a"}, 1) end,
			function() xml.child({name = "a"}, nil) end, function() xml.children({name = "a"}, 1) end,
			function() xml.find({name = "a"}, 1) end, function() xml.find({name = "a"}, "a", 1) end}) do
			local ok, perr = pcall(f)
			check(not ok, "raises")
			out[#out + 1] = code(perr)
		end
		for _, path in ipairs({"q:a", "a[1]", ""}) do
			local found, ferr = xml.find({name = "a"}, path)
			check(found == nil, "no result for a bad path")
			eq(ferr:kind(), errors.INVALID, "kind")
			out[#out + 1] = code(ferr)
		end
		local found, ferr = xml.find({name = "a", children = {{name = "b"}}}, "b")
		check(ferr == nil and #found == 1, "find returns a nil error")
		return table.concat(out, ",")`)
	assert.Equal(t, "argument,argument,argument,argument,argument,argument,argument,argument,argument,argument,argument,argument,namespace,argument,argument", vals[0].String())
}

func TestEncode(t *testing.T) {
	cases := []struct{ name, tree, opts, want string }{
		{"empty", `{name = "a"}`, "", `<a/>`},
		{"unknown fields are ignored", `{name = "a", extra = {1, 2}, comment = "x"}`, "", `<a/>`},
		{"text escaping", `{name = "a", text = "a<b>&\"c'\r\n"}`, "", "<a>a&lt;b&gt;&amp;\"c'&#13;\n</a>"},
		{"attribute escaping and order", `{name = "a", attrs = {b = "2", a = "<\t\"", ["{v}z"] = "3", ["{u}y"] = "4"}}`, "",
			`<a xmlns:ns1="u" xmlns:ns2="v" a="&lt;&#9;&quot;" b="2" ns1:y="4" ns2:z="3"/>`},
		{"default namespace", `{name = "a", ns = "u", children = {{name = "b", ns = "u"}}}`, "", `<a xmlns="u"><b/></a>`},
		{"no namespace under default", `{name = "a", ns = "u", children = {{name = "b"}}}`, "", `<a xmlns="u"><b xmlns=""/></a>`},
		{"declared prefix", `{name = "a", ns = "u", xmlns = {p = "u"}}`, "", `<p:a xmlns:p="u"/>`},
		{"preferred bindings", `{name = "a", ns = "u", children = {{name = "b", ns = "v"}}}`, `{ns = {s = "u", t = "v"}}`,
			`<s:a xmlns:s="u" xmlns:t="v"><t:b/></s:a>`},
		{"declaration", `{name = "a"}`, `{declaration = true}`, `<?xml version="1.0" encoding="UTF-8"?><a/>`},
		{"indent", `{name = "a", children = {{name = "b", text = "x"}, {name = "c", children = {{name = "d"}}}}}`, `{indent = "  "}`,
			"<a>\n  <b>x</b>\n  <c>\n    <d/>\n  </c>\n</a>"},
		{"indent leaves mixed content", `{name = "a", children = {{name = "p", children = {"x", {name = "b"}}}}}`, `{indent = "\t"}`,
			"<a>\n\t<p>x<b/></p>\n</a>"},
		{"declaration line with indent", `{name = "a"}`, `{declaration = true, indent = " "}`, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<a/>"},
		{"xml attribute", `{name = "a", attrs = {["{http://www.w3.org/XML/1998/namespace}lang"] = "en"}}`, "", `<a xml:lang="en"/>`},
	}
	e := newEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			if opts == "" {
				opts = "nil"
			}
			vals := e.run(`local out, err = xml.encode(` + tc.tree + `, ` + opts + `)
				check(err == nil, tostring(err))
				local back, derr = xml.decode(out)
				check(derr == nil, tostring(derr))
				local again = xml.encode(back, ` + opts + `)
				return out, again`)
			assert.Equal(t, tc.want, vals[0].String())
			if !strings.Contains(tc.opts, "indent") {
				assert.Equal(t, tc.want, vals[1].String(), "encoding is stable across a round trip")
			}
		})
	}
}

func TestEncodeRejectsOutsideNormalForm(t *testing.T) {
	cases := map[string]string{
		"text and children":         `{name = "a", text = "x", children = {{name = "b"}}}`,
		"children without element":  `{name = "a", children = {"x"}}`,
		"empty string child":        `{name = "a", children = {"", {name = "b"}}}`,
		"adjacent strings":          `{name = "a", children = {"x", "y", {name = "b"}}}`,
		"foreign child":             `{name = "a", children = {1}}`,
		"sparse children":           `{name = "a", children = {[1] = {name = "b"}, [3] = {name = "c"}}}`,
		"children map":              `{name = "a", children = {x = {name = "b"}}}`,
		"empty children":            `{name = "a", children = {}}`,
		"empty attrs":               `{name = "a", attrs = {}}`,
		"empty xmlns":               `{name = "a", xmlns = {}}`,
		"empty text":                `{name = "a", text = ""}`,
		"missing name":              `{text = "x"}`,
		"numeric name":              `{name = 1}`,
		"invalid name":              `{name = "1a"}`,
		"colon in name":             `{name = "p:a"}`,
		"numeric attribute":         `{name = "a", attrs = {x = 1}}`,
		"numeric attribute key":     `{name = "a", attrs = {[1] = "x"}}`,
		"invalid attribute key":     `{name = "a", attrs = {["{u"] = "1"}}`,
		"attribute named xmlns":     `{name = "a", attrs = {xmlns = "u"}}`,
		"invalid character":         `{name = "a", text = "\0"}`,
		"malformed UTF-8":           `{name = "a", text = "\255"}`,
		"numeric namespace":         `{name = "a", ns = 1}`,
		"numeric text":              `{name = "a", text = 1}`,
		"invalid child element":     `{name = "a", children = {{name = "b", attrs = {}}}}`,
		"numeric declaration value": `{name = "a", xmlns = {p = 1}}`,
	}
	e := newEnv(t)
	for name, tree := range cases {
		t.Run(name, func(t *testing.T) {
			vals := e.run(`local out, err = xml.encode(` + tree + `)
				check(out == nil, "no output")
				return code(err), err:kind()`)
			assert.Equal(t, "element", vals[0].String())
			assert.Equal(t, "Invalid", vals[1].String())
		})
	}
	for name, tree := range map[string]string{
		"xml binding":          `{name = "a", xmlns = {xml = "http://www.w3.org/XML/1998/namespace"}}`,
		"prefix undeclaration": `{name = "a", xmlns = {p = ""}}`,
		"reserved namespace":   `{name = "a", ns = "http://www.w3.org/2000/xmlns/"}`,
	} {
		t.Run(name, func(t *testing.T) {
			vals := e.run(`local _, err = xml.encode(` + tree + `)
				return code(err)`)
			assert.Equal(t, "namespace", vals[0].String())
		})
	}
	vals := e.run(`local t = {name = "a"}
		t.children = {t}
		local _, err = xml.encode(t)
		return code(err), err:details().limit`)
	assert.Equal(t, "limit", vals[0].String(), "a cyclic table reaches the depth limit")
}

func TestHelpers(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`
		local doc = check(xml.decode('<s:root xmlns:s="urn:s" xmlns:x="urn:x" id="1" x:id="2">' ..
			'<s:item n="a">one<b>two</b></s:item>tail<item n="b"/><x:item n="c"><s:item n="d"/></x:item></s:root>'))
		eq(xml.text(doc), "onetwotail", "text")
		eq(xml.attr(doc, "id"), "1", "attr")
		eq(xml.attr(doc, "id", "urn:x"), "2", "qualified attr")
		eq(xml.attr(doc, "missing"), nil, "missing attr")
		eq(xml.child(doc, "item").attrs.n, "a", "first child in any namespace")
		eq(xml.child(doc, "item", "").attrs.n, "b", "child without namespace")
		eq(xml.child(doc, "item", "urn:x").attrs.n, "c", "child in namespace")
		eq(xml.child(doc, "none"), nil, "no child")
		eq(#xml.children(doc), 3, "all children")
		eq(#xml.children(doc, "item"), 3, "named children")
		eq(#xml.children(doc, "item", "urn:s"), 1, "namespaced children")
		eq(#xml.children(doc, nil, "urn:x"), 1, "children by namespace")
		local found = xml.find(doc, "*:item/s:item", {ns = {s = "urn:s"}})
		eq(#found, 1, "find")
		eq(found[1].attrs.n, "d", "found record")
		eq(#xml.find(doc, "item"), 1, "bare name matches no namespace")
		eq(#xml.find(doc, "*"), 3, "wildcard")
		eq(#xml.find(doc, "q:*", {ns = {q = "urn:x"}}), 1, "namespace wildcard")
		local local_name, ns, err = xml.resolve(doc, "x:Type")
		check(err == nil, tostring(err))
		eq(local_name, "Type", "resolved local")
		eq(ns, "urn:x", "resolved namespace")
		local_name, ns = xml.resolve(doc, "Plain")
		eq(local_name, "Plain", "unprefixed")
		eq(ns, nil, "no default namespace")
		local _, _, rerr = xml.resolve(doc, "q:Type")
		eq(code(rerr), "namespace", "unbound prefix")
		_, _, rerr = xml.resolve(doc, "1bad")
		eq(code(rerr), "syntax", "invalid qname")
		eq(xml.text({name = "a"}), "", "empty text")
		return true`)
	assert.Equal(t, "true", vals[0].String())
}

func TestDecodeStringsAreInternedPerTree(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local doc = xml.decode('<r>` + strings.Repeat(`<i k="v"/>`, 3) + `</r>')
		return doc.children[1].name, doc.children[3].attrs.k`)
	assert.Equal(t, "i", vals[0].String())
	assert.Equal(t, "v", vals[1].String())
}

func TestDecodeWithoutContext(t *testing.T) {
	e := newBareEnv(t)
	vals := e.run(`local doc = check(xml.decode("<a><b>x</b></a>"))
		local out = check(xml.encode(doc))
		local r = check(xml.reader(out))
		local kinds = {}
		while true do
			local kind, err = r:next()
			check(err == nil, tostring(err))
			if not kind then break end
			kinds[#kinds + 1] = kind
		end
		local w = check(xml.writer())
		w:start_element("z")
		w:end_element()
		return out, table.concat(kinds, ","), w:finish()`)
	assert.Equal(t, "<a><b>x</b></a>", vals[0].String())
	assert.Equal(t, "start,start,text,end,end", vals[1].String())
	assert.Equal(t, "<z/>", vals[2].String())
	assert.Zero(t, e.yieldCount())
}

func TestLimitErrorDetails(t *testing.T) {
	e := newEnv(t)
	vals := e.run(`local _, err = xml.decode("<a>` + strings.Repeat("x", 100) + `</a>", {max_node_bytes = 50})
		local d = err:details()
		return d.code, d.limit, d.path[1]`)
	assert.Equal(t, []string{"limit", "max_node_bytes", "a"}, []string{vals[0].String(), vals[1].String(), vals[2].String()})
}
