// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestDensifyMatchesProbeOrder(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 41))
	for k := 1; k <= 300; k++ {
		if gcd(uint64(k), minhashStride) != 1 {
			continue
		}
		for trial := 0; trial < 30; trial++ {
			bins := make([]uint32, k)
			for i := range bins {
				bins[i] = emptyBin
				if r.IntN(8) == 0 {
					bins[i] = r.Uint32() % 1000000
				}
			}
			bins[r.IntN(k)] = r.Uint32() % 1000000
			want := slices.Clone(bins)
			for i, v := range bins {
				if v != emptyBin {
					continue
				}
				start := (uint64(i+1) * minhashProbe) % minhashModulus
				for a := uint64(1); a <= uint64(k); a++ {
					if found := bins[(start+minhashStride*a)%uint64(k)]; found != emptyBin {
						want[i] = found
						break
					}
				}
			}
			if got := densify(bins); !slices.Equal(want, got) {
				t.Fatalf("k=%d trial=%d: got %v, want %v", k, trial, got, want)
			}
		}
	}
}

func TestMinHashDisjointShortInputs(t *testing.T) {
	c := DefaultMinHashConfig()
	a := signatureOf(t, c, "a")
	b := signatureOf(t, c, "b")
	j, err := MinHashSimilarity(a.Sketch, b.Sketch)
	if err != nil {
		t.Fatal(err)
	}
	if j != 0 {
		t.Fatalf("disjoint singleton token sets have similarity %v, want 0", j)
	}
}

func TestMinHashRejectsOversizedSketch(t *testing.T) {
	if _, err := DecodeMinHashSketch(make([]byte, 4*(MinHashMaxK+1))); err == nil {
		t.Fatal("accepted an oversized serialized sketch")
	}
	c := DefaultMinHashConfig()
	c.Bands, c.Rows = MinHashMaxK+1, 1
	if _, err := MinHashBands(make([]uint32, MinHashMaxK+1), c); err == nil {
		t.Fatal("accepted an oversized sketch for banding")
	}
}

func TestDensifyMaximumSparseSketch(t *testing.T) {
	bins := make([]uint32, MinHashMaxK)
	for i := range bins {
		bins[i] = emptyBin
	}
	bins[len(bins)/2] = 123456789
	for i, v := range densify(bins) {
		if v != 123456789 {
			t.Fatalf("bin %d = %d, want 123456789", i, v)
		}
	}
}

func BenchmarkMinHashSparse(b *testing.B) {
	for _, k := range []int{128, 1024, 8192, MinHashMaxK} {
		b.Run(fmt.Sprint(k), func(b *testing.B) {
			c := DefaultMinHashConfig()
			c.K = k
			m, err := NewMinHasher(c, false)
			if err != nil {
				b.Fatal(err)
			}
			m.Write("one")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = m.Sum()
			}
		})
	}
}
