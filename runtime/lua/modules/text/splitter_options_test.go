// SPDX-License-Identifier: MPL-2.0

package text

import (
	"strconv"
	"testing"

	"github.com/tmc/langchaingo/textsplitter"
	lua "github.com/wippyai/go-lua"
)

func TestSplitterNumericOptions(t *testing.T) {
	for _, kind := range []string{"integer", "float"} {
		for _, overlap := range []int{0, 7} {
			t.Run(kind+"/overlap="+strconv.Itoa(overlap), func(t *testing.T) {
				l := lua.NewState()
				defer l.Close()
				options := l.NewTable()
				if kind == "integer" {
					options.RawSetString("chunk_size", lua.LInteger(64))
					options.RawSetString("chunk_overlap", lua.LInteger(overlap))
				} else {
					options.RawSetString("chunk_size", lua.LNumber(64))
					options.RawSetString("chunk_overlap", lua.LNumber(overlap))
				}
				recursive := textsplitter.NewRecursiveCharacter(parseRecursiveOptions(options)...)
				if recursive.ChunkSize != 64 || recursive.ChunkOverlap != overlap {
					t.Errorf("recursive options: size=%d overlap=%d; want size=64 overlap=%d", recursive.ChunkSize, recursive.ChunkOverlap, overlap)
				}
				markdown := textsplitter.NewMarkdownTextSplitter(parseMarkdownOptions(options)...)
				if markdown.ChunkSize != 64 || markdown.ChunkOverlap != overlap {
					t.Errorf("markdown options: size=%d overlap=%d; want size=64 overlap=%d", markdown.ChunkSize, markdown.ChunkOverlap, overlap)
				}
			})
		}
	}
}

func TestSplitterZeroOverlapPreservesText(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	err := l.DoString(`
        local input = string.rep("alpha beta gamma delta epsilon ", 20):gsub(" $", "")
        for _, make in ipairs({text.splitter.recursive, text.splitter.markdown}) do
            local splitter, err = make({chunk_size = 64, chunk_overlap = 0})
            assert(not err, tostring(err))
            local chunks, split_err = splitter:split_text(input)
            assert(not split_err, tostring(split_err))
            assert(#chunks > 1, "configured size does not split text")
            for _, chunk in ipairs(chunks) do assert(#chunk <= 64, "configured chunk size is ignored") end
            assert(table.concat(chunks, " ") == input, "zero overlap repeats source text")
        end
    `)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSplitterIntegerOptionsPreservePrecision(t *testing.T) {
	sizes := []int{int(^uint(0) >> 1)}
	if strconv.IntSize == 64 {
		large := int64(1<<53 + 1)
		sizes = append(sizes, int(large))
	}
	for _, size := range sizes {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			options := l.NewTable()
			options.RawSetString("chunk_size", lua.LInteger(size))
			options.RawSetString("chunk_overlap", lua.LInteger(size-1))
			recursive := textsplitter.NewRecursiveCharacter(parseRecursiveOptions(options)...)
			if recursive.ChunkSize != size || recursive.ChunkOverlap != size-1 {
				t.Errorf("recursive options lost integer precision: size=%d overlap=%d; want size=%d overlap=%d", recursive.ChunkSize, recursive.ChunkOverlap, size, size-1)
			}
			markdown := textsplitter.NewMarkdownTextSplitter(parseMarkdownOptions(options)...)
			if markdown.ChunkSize != size || markdown.ChunkOverlap != size-1 {
				t.Errorf("markdown options lost integer precision: size=%d overlap=%d; want size=%d overlap=%d", markdown.ChunkSize, markdown.ChunkOverlap, size, size-1)
			}
		})
	}
}
