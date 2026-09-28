// SPDX-License-Identifier: MPL-2.0

package json

import "testing"

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
