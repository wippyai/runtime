// SPDX-License-Identifier: MPL-2.0

package minhash

import (
	"os"
	"strings"
	"testing"
)

func TestSpecExampleRuns(t *testing.T) {
	spec, err := os.ReadFile("spec.md")
	if err != nil {
		t.Fatal(err)
	}
	_, example, ok := strings.Cut(strings.ReplaceAll(string(spec), "\r\n", "\n"), "## Example")
	if !ok {
		t.Fatal("spec.md has no Example section")
	}
	_, code, ok := strings.Cut(example, "```lua\n")
	if !ok {
		t.Fatal("Example section has no Lua block")
	}
	code, _, _ = strings.Cut(code, "```")
	l := newState(t)
	if err := l.DoString(`function require(name) return ({ minhash = minhash })[name] end`); err != nil {
		t.Fatal(err)
	}
	if err := l.DoString(code); err != nil {
		t.Fatal(err)
	}
}
