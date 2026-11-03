// SPDX-License-Identifier: MPL-2.0

package json

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func benchmarkEncode(b *testing.B, data []byte) {
	value, err := Decode(data)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode_Simple(b *testing.B)       { benchmarkEncode(b, simpleJSON) }
func BenchmarkEncode_Complex(b *testing.B)      { benchmarkEncode(b, complexJSON) }
func BenchmarkEncode_LLMRequest(b *testing.B)   { benchmarkEncode(b, llmRequestJSON) }
func BenchmarkEncode_LargeDataset(b *testing.B) { benchmarkEncode(b, largeDatasetJSON) }
func BenchmarkEncode_LargeArray(b *testing.B)   { benchmarkEncode(b, largeArrayJSON) }

func BenchmarkEncode_NumericHashKeys(b *testing.B) {
	table := lua.CreateTable(0, 64)
	for i := 1; i <= 64; i++ {
		table.RawSetH(lua.LNumber(i), lua.LNumber(i))
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Encode(table); err != nil {
			b.Fatal(err)
		}
	}
}
