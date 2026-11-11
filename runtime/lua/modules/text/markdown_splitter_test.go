// SPDX-License-Identifier: MPL-2.0

package text

import (
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func splitMarkdown(t *testing.T, options map[string]lua.LValue, input string) []string {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	opts := l.NewTable()
	for k, v := range options {
		opts.RawSetString(k, v)
	}
	l.SetGlobal("options", opts)
	l.SetGlobal("input", lua.LString(input))
	err := l.DoString(`
		local splitter, err = text.splitter.markdown(options)
		assert(not err, tostring(err))
		local chunks, split_err = splitter:split_text(input)
		assert(not split_err, tostring(split_err))
		result = chunks
	`)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []string
	l.GetGlobal("result").(*lua.LTable).ForEach(func(_, v lua.LValue) {
		chunks = append(chunks, v.String())
	})
	return chunks
}

func TestSplitterMarkdownBoundsTextWithoutSeparators(t *testing.T) {
	const size, overlap = 200, 20
	input := "Opening paragraph.\n\n" + strings.Repeat(".", 50000) + "\n\nA token follows: " + strings.Repeat("x", 50000)
	chunks := splitMarkdown(t, map[string]lua.LValue{
		"chunk_size":    lua.LNumber(size),
		"chunk_overlap": lua.LNumber(overlap),
	}, input)
	dots, xs := 0, 0
	for i, chunk := range chunks {
		if n := len([]rune(chunk)); n > size+overlap {
			t.Fatalf("chunk %d holds %d characters; want at most %d", i, n, size+overlap)
		}
		for _, r := range chunk {
			switch r {
			case '.':
				dots++
			case 'x':
				xs++
			}
		}
	}
	if dots < 50000 || xs < 50000 {
		t.Fatalf("chunks lose text: %d dots and %d x of 50000 each", dots, xs)
	}
}

func TestSplitterMarkdownSplitsParagraphsAtConfiguredSeparators(t *testing.T) {
	separators := &lua.LTable{}
	separators.Append(lua.LString(";"))
	input := strings.Repeat("abcdefghi;", 99) + "abcdefghi"
	chunks := splitMarkdown(t, map[string]lua.LValue{
		"chunk_size":    lua.LNumber(50),
		"chunk_overlap": lua.LNumber(0),
		"separators":    separators,
	}, input)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks; want the paragraph split", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) > 50 {
			t.Fatalf("chunk %d holds %d characters; want at most 50", i, len(chunk))
		}
		if (len(chunk)+1)%10 != 0 || chunk[0] != 'a' || chunk[len(chunk)-1] != 'i' {
			t.Fatalf("chunk %d %q splits inside a segment between separators", i, chunk)
		}
	}
}
