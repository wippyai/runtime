package uniqid

import (
	"sync"
	"testing"
)

func TestUniqIDGenerator(t *testing.T) {
	t.Run("generates incremental ids", func(t *testing.T) {
		gen := NewGenerator()

		expected := []string{
			"0x00001",
			"0x00002",
			"0x00003",
		}

		for _, want := range expected {
			got := gen.Generate()
			if got != want {
				t.Errorf("Generate() = %v, want %v", got, want)
			}
		}
	})

	t.Run("reset resets counter", func(t *testing.T) {
		gen := NewGenerator()

		// Generate some IDs
		gen.Generate() // 0x00001
		gen.Generate() // 0x00002

		// Reset
		gen.Reset()

		// Should start from 1 again
		got := gen.Generate()
		want := "0x00001"
		if got != want {
			t.Errorf("after Reset(), Generate() = %v, want %v", got, want)
		}
	})

	t.Run("concurrent generation produces unique ids", func(t *testing.T) {
		gen := NewGenerator()

		// Generate IDs concurrently
		count := 1000
		var wg sync.WaitGroup
		seen := make(map[string]bool)
		var mu sync.Mutex

		wg.Add(count)
		for i := 0; i < count; i++ {
			go func() {
				defer wg.Done()
				id := gen.Generate()

				mu.Lock()
				if seen[id] {
					t.Errorf("duplicate Source generated: %v", id)
				}
				seen[id] = true
				mu.Unlock()
			}()
		}
		wg.Wait()

		// Verify count
		if len(seen) != count {
			t.Errorf("generated %d unique IDs, want %d", len(seen), count)
		}

		// Verify format of generated IDs
		for id := range seen {
			// Length should be 7 (0x + 5 hex digits)
			if len(id) != 7 {
				t.Errorf("invalid Source length for %v, got %d, want 7", id, len(id))
			}
			// Should start with 0x
			if id[:2] != "0x" {
				t.Errorf("invalid Source prefix for %v, want '0x'", id)
			}
		}
	})

	t.Run("large number of generations", func(t *testing.T) {
		gen := NewGenerator()

		// Generate a large number of IDs
		count := uint64(65535) // Test with a significant number
		var last string

		for i := uint64(0); i < count; i++ {
			last = gen.Generate()
		}

		// Verify the last generated Source matches our count
		want := "0x0ffff" // hex for 65535
		if last != want {
			t.Errorf("after %d generations, last Source = %v, want %v", count, last, want)
		}
	})

	t.Run("format at boundaries", func(t *testing.T) {
		tests := []struct {
			want    string
			counter uint64
		}{
			{"0x00001", 0},                     // first ID (5 digits)
			{"0xfffff", 0xFFFFE},               // 5 digits max
			{"0x100000", 0xFFFFF},              // 6 digits start
			{"0x01000000", 0xFFFFFF},           // 8 digits start
			{"0x0000000100000000", 0xFFFFFFFF}, // 16 digits start
		}

		for _, tt := range tests {
			gen := &Generator{counter: tt.counter}
			got := gen.Generate()
			if got != tt.want {
				t.Errorf("Generate() at counter %x = %v, want %v", tt.counter, got, tt.want)
			}
		}
	})
}

func BenchmarkGenerate(b *testing.B) {
	gen := NewGenerator()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gen.Generate()
	}
}

func BenchmarkGenerateParallel(b *testing.B) {
	gen := NewGenerator()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = gen.Generate()
		}
	})
}
