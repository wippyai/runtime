// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
)

// TestEveryLimitAtItsBoundary runs each reader and writer limit at limit-1, limit and
// limit+1, where limit is the exact size of the input as the spec defines it.
func TestEveryLimitAtItsBoundary(t *testing.T) {
	cases := []struct {
		option string
		call   string
		doc    string
		size   int
	}{
		{"max_depth", "decode", `<a><b><c/></b></a>`, 3},
		{"max_attrs", "decode", `<a x="1" y="2" xmlns:p="u"/>`, 3},
		{"max_namespaces", "decode", `<a xmlns:p="1"><b xmlns:q="2"/></a>`, 2},
		{"max_tag_bytes", "decode", `<a x="12345"/>`, len(`<a x="12345"/>`)},
		{"max_token_bytes", "decode", "<a>é</a>", len("é")},
		{"max_node_bytes", "decode", `<a>0123456789</a>`, len(`<a>0123456789</a>`)},
		{"max_bytes", "decode", `<a>0123456789</a>`, len(`<a>0123456789</a>`)},
		{"max_depth", "encode", `{name = "a", children = {{name = "b", children = {{name = "c"}}}}}`, 3},
		{"max_attrs", "encode", `{name = "a", attrs = {x = "1", y = "2"}, xmlns = {p = "u"}}`, 3},
		{"max_namespaces", "encode", `{name = "a", xmlns = {p = "1"}, children = {{name = "b", xmlns = {q = "2"}}}}`, 2},
		{"max_tag_bytes", "encode", `{name = "a", attrs = {x = "12345"}}`, len(`<a x="12345"/>`)},
		{"max_node_bytes", "encode", `{name = "a", text = "0123456789"}`, len(`<a>0123456789</a>`)},
	}
	e := newEnv(t)
	for _, tc := range cases {
		t.Run(tc.call+"/"+tc.option, func(t *testing.T) {
			var got []string
			for _, limit := range []int{tc.size - 1, tc.size, tc.size + 1} {
				var script string
				if tc.call == "decode" {
					e.global("doc", lua.LString(tc.doc))
					script = fmt.Sprintf(`local _, err = xml.decode(doc, {%s = %d})`, tc.option, limit)
				} else {
					script = fmt.Sprintf(`local _, err = xml.encode(%s, {%s = %d})`, tc.doc, tc.option, limit)
				}
				vals := e.run(script + `
					if err == nil then return "ok" end
					return code(err) .. ":" .. tostring(err:details().limit)`)
				got = append(got, vals[0].String())
			}
			assert.Equal(t, []string{"limit:" + tc.option, "ok", "ok"}, got)
		})
	}
	vals := e.run(`local _, at = xml.writer(nil, {indent = string.rep(" ", 256)})
		local _, over = xml.writer(nil, {indent = string.rep(" ", 257)})
		return tostring(at), code(over)`)
	assert.Equal(t, []string{"nil", "argument"}, []string{vals[0].String(), vals[1].String()})
}

func TestReportedMarkupTagBoundary(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		doc, opts string
		size      int
	}{
		{`<a><!--abc--></a>`, "comments = true", len(`<!--abc-->`)},
		{`<a><?t data?></a>`, "instructions = true", len(`<?t data?>`)},
	} {
		var got []string
		for _, limit := range []int{tc.size - 1, tc.size, tc.size + 1} {
			e.global("doc", lua.LString(tc.doc))
			vals := e.run(fmt.Sprintf(`local r = check(xml.reader(doc, {%s, max_tag_bytes = %d}))
				while true do
					local kind, err = r:next()
					if err then return err:details().limit end
					if not kind then return "ok" end
				end`, tc.opts, limit))
			got = append(got, vals[0].String())
		}
		assert.Equal(t, []string{"max_tag_bytes", "ok", "ok"}, got, tc.doc)
	}
	vals := e.run(`local function kept(n)
			local doc = xml.decode("<a>" .. string.rep(" ", 8) .. "<b/></a>", {whitespace = "strip", max_token_bytes = n})
			return doc.children and #doc.children or 0
		end
		return kept(7) .. "/" .. kept(8) .. "/" .. kept(9)`)
	assert.Equal(t, "2/1/1", vals[0].String(), "a whitespace run is stripped only when it fits max_token_bytes")
}
