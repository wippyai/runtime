// SPDX-License-Identifier: MPL-2.0

package text

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/textsplitter"
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

func TestSplitterMarkdownUnicodeFallbackPreservesText(t *testing.T) {
	input := strings.Repeat("é猫🙂", 800)
	chunks := splitMarkdown(t, map[string]lua.LValue{
		"chunk_size":    lua.LNumber(200),
		"chunk_overlap": lua.LNumber(0),
	}, input)
	for i, chunk := range chunks {
		if size := len([]rune(chunk)); size > 200 {
			t.Fatalf("chunk %d has %d characters, want at most 200", i, size)
		}
	}
	if got := strings.Join(chunks, ""); got != input {
		t.Fatal("zero-overlap Unicode fallback changes the source text")
	}
}

func TestSplitterMarkdownPreservesExistingFormatOptions(t *testing.T) {
	input := "# Parent\n\n" + strings.Repeat("alpha beta gamma delta. ", 20) +
		"\n\n## Child\n\n> Quote\n\n- First\n- Second\n\n" +
		"```lua\nreturn 42\n```\n\n[link][ref]\n\n[ref]: https://example.test\n\n" +
		"| Name | Value |\n| --- | --- |\n| First | 1 |\n| Second | 2 |\n"
	flags := []string{"code_blocks", "reference_links", "heading_hierarchy", "join_table_rows"}
	for mask := range 1 << len(flags) {
		t.Run(strconv.Itoa(mask), func(t *testing.T) {
			options := map[string]lua.LValue{
				"chunk_size":    lua.LNumber(64),
				"chunk_overlap": lua.LNumber(8),
			}
			l := lua.NewState()
			defer l.Close()
			table := l.NewTable()
			for i, name := range flags {
				options[name] = lua.LBool(mask&(1<<i) != 0)
			}
			for name, value := range options {
				table.RawSetString(name, value)
			}
			previous := textsplitter.NewMarkdownTextSplitter(parseMarkdownOptions(table)...)
			want, err := previous.SplitText(input)
			if err != nil {
				t.Fatal(err)
			}
			got := splitMarkdown(t, options, input)
			if len(got) != len(want) {
				t.Fatalf("chunk count = %d, want previous behavior's %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("chunk %d changes existing formatting: got %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}
