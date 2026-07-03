// SPDX-License-Identifier: MPL-2.0

package placeholder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseBody(t *testing.T) {
	cases := []struct {
		body       string
		name       string
		def        string
		hasDefault bool
		ok         bool
	}{
		{body: "env:FOO", name: "FOO", ok: true},
		{body: "env:FOO|bar", name: "FOO", def: "bar", hasDefault: true, ok: true},
		{body: "env:app:key", name: "app:key", ok: true},
		{body: "env:ns.sub:key-1", name: "ns.sub:key-1", ok: true},
		{body: "FOO_BAR|9", name: "FOO_BAR", def: "9", hasDefault: true, ok: true},
		// Bare shorthand without a default pipe is not a reference, so ${VAR}
		// spans inside embedded shell or template source stay untouched.
		{body: "FOO", ok: false},
		{body: "env:", ok: false},
		{body: "lower", ok: false},
		{body: "1ABC", ok: false},
		{body: "some text", ok: false},
		{body: "MixedCase", ok: false},
	}
	for _, c := range cases {
		name, def, hasDefault, ok := parseBody(c.body)
		assert.Equal(t, c.ok, ok, "ok for %q", c.body)
		if c.ok {
			assert.Equal(t, c.name, name, "name for %q", c.body)
			assert.Equal(t, c.def, def, "def for %q", c.body)
			assert.Equal(t, c.hasDefault, hasDefault, "hasDefault for %q", c.body)
		}
	}
}

func TestExtractNames(t *testing.T) {
	assert.Nil(t, ExtractNames("no placeholders here"))
	assert.Nil(t, ExtractNames(""))
	assert.Equal(t, []string{"FOO"}, ExtractNames("${env:FOO}"))
	assert.Equal(t, []string{"FOO", "BAR"}, ExtractNames("${FOO|1}-${env:BAR|x}"))
	assert.Equal(t, []string{"app:key"}, ExtractNames("prefix ${env:app:key} suffix"))
	// Duplicates collapse, first-occurrence order preserved.
	assert.Equal(t, []string{"A", "B"}, ExtractNames("${A|1}${B|2}${A|1}"))
	// Escapes, malformed spans, and bare shorthand contribute no names.
	assert.Nil(t, ExtractNames("$${env:FOO} ${lower} ${1BAD} ${BARE}"))
}

func TestParse(t *testing.T) {
	// Pure literal, no placeholder marker.
	assert.Equal(t, []Segment{{Literal: "plain"}}, Parse("plain"))

	// Whole-value reference with default.
	assert.Equal(t, []Segment{{Name: "FOO", Default: "9", HasDefault: true, IsRef: true}}, Parse("${env:FOO|9}"))

	// Mixed literal and reference segments.
	assert.Equal(t, []Segment{
		{Literal: "a-"},
		{Name: "FOO", Default: "x", HasDefault: true, IsRef: true},
		{Literal: "-b"},
	}, Parse("a-${FOO|x}-b"))

	// Bare shorthand without a default pipe stays literal.
	assert.Equal(t, []Segment{{Literal: "cp ${SRC} ${DST}"}}, Parse("cp ${SRC} ${DST}"))

	// The $${ escape yields a literal ${ and no reference.
	assert.Equal(t, []Segment{{Literal: "${env:FOO}"}}, Parse("$${env:FOO}"))

	// Unrecognized bodies are preserved as literal text.
	assert.Equal(t, []Segment{{Literal: "${lower}"}}, Parse("${lower}"))
}
