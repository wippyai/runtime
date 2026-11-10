// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

// specExample returns the Lua code of the Example section of spec.md.
func specExample(t *testing.T) string {
	t.Helper()
	spec, err := os.ReadFile("spec.md")
	require.NoError(t, err)
	_, example, ok := strings.Cut(string(spec), "## Example")
	require.True(t, ok)
	_, code, ok := strings.Cut(example, "```lua\n")
	require.True(t, ok)
	code, _, ok = strings.Cut(code, "```")
	require.True(t, ok)
	return code
}

func TestSpecExampleRuns(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.l.DoString(`function require(name) return ({xml = xml})[name] end`))
	feed := `<s:catalog xmlns:s="urn:shop"><s:item id="1">one</s:item><s:item id="2">two</s:item></s:catalog>`
	orders := `<items><item status="active">a</item><item status="retired">b</item><item status="active">c</item></items>`
	e.global("feed", e.stream(strings.NewReader(feed)))
	e.global("orders", e.stream(strings.NewReader(orders)))
	e.global("orders_text", lua.LString(orders))
	var out bytes.Buffer
	sink, _ := e.sink(&out)
	e.global("out", sink)
	fn, err := e.l.LoadString(specExample(t) + "\nreturn each_item, active_items, total")
	require.NoError(t, err)
	e.global("example", fn)
	vals := e.run(`local each_item, active_items, total = example()
		eq(total.text, "12.50", "decoded child")
		local seen = {}
		check(each_item(feed, function(id, text) seen[#seen + 1] = id .. "=" .. text end))
		eq(table.concat(seen, ","), "1=one,2=two", "records")
		check(active_items(orders, out))
		return active_items(orders_text, nil)`)
	assert.Equal(t, "<items>\n  <item status=\"active\">a</item>\n  <item status=\"active\">c</item>\n</items>", out.String())
	assert.Equal(t, out.String(), vals[0].String(), "an in-memory writer returns the same document")
}
