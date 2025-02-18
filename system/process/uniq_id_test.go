package process

import (
	"sync"
	"testing"
)

func TestUniqIDGenerator(t *testing.T) {
	t.Run("generates incremental ids", func(t *testing.T) {
		gen := NewUniqIDGenerator()

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
		gen := NewUniqIDGenerator()

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
		gen := NewUniqIDGenerator()

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
					t.Errorf("duplicate ID generated: %v", id)
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
				t.Errorf("invalid ID length for %v, got %d, want 7", id, len(id))
			}
			// Should start with 0x
			if id[:2] != "0x" {
				t.Errorf("invalid ID prefix for %v, want '0x'", id)
			}
		}
	})

	t.Run("large number of generations", func(t *testing.T) {
		gen := NewUniqIDGenerator()

		// Generate a large number of IDs
		count := uint64(65535) // Test with a significant number
		var last string

		for i := uint64(0); i < count; i++ {
			last = gen.Generate()
		}

		// Verify the last generated ID matches our count
		want := "0x0ffff" // hex for 65535
		if last != want {
			t.Errorf("after %d generations, last ID = %v, want %v", count, last, want)
		}
	})
}
