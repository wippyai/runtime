// SPDX-License-Identifier: MPL-2.0

package finder

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// Benchmark helpers

type benchMockRegistry struct {
	entries []registry.Entry
	version uint
}

func (m *benchMockRegistry) GetAllEntries() ([]registry.Entry, error) {
	return m.entries, nil
}

func (m *benchMockRegistry) GetEntry(id registry.ID) (registry.Entry, error) {
	for _, entry := range m.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return registry.Entry{}, errors.New("not found")
}

func (m *benchMockRegistry) Current() (registry.Version, error) {
	return &benchMockVersion{id: m.version}, nil
}

func (m *benchMockRegistry) History() registry.History { return nil }
func (m *benchMockRegistry) Apply(_ context.Context, _ registry.ChangeSet) (registry.Version, error) {
	return nil, nil //nolint:nilnil // mock stub
}
func (m *benchMockRegistry) ApplyVersion(_ context.Context, _ registry.Version) error { return nil }

type benchMockVersion struct {
	id uint
}

func (m *benchMockVersion) ID() uint                   { return m.id }
func (m *benchMockVersion) Previous() registry.Version { return nil }
func (m *benchMockVersion) Next() registry.Version     { return nil }
func (m *benchMockVersion) String() string             { return fmt.Sprintf("v%d", m.id) }

// generateEntries creates test entries with various metadata patterns
func generateEntries(count int) []registry.Entry {
	entries := make([]registry.Entry, count)
	kinds := []string{"service", "database", "storage", "cache", "queue"}
	tags := [][]string{
		{"api", "rest", "backend"},
		{"sql", "postgres", "persistence"},
		{"s3", "files", "blob"},
		{"redis", "memory", "fast"},
		{"rabbitmq", "messaging", "async"},
	}

	for i := 0; i < count; i++ {
		kindIdx := i % len(kinds)
		entries[i] = registry.Entry{
			ID:   registry.ID{NS: fmt.Sprintf("ns%d", i%10), Name: fmt.Sprintf("entry-%d", i)},
			Kind: kinds[kindIdx],
			Meta: attrs.Bag{
				"enabled":     i%2 == 0,
				"port":        8000 + i,
				"description": fmt.Sprintf("Test %s service number %d", kinds[kindIdx], i),
				"version":     fmt.Sprintf("v%d.%d.%d", i/100, (i/10)%10, i%10),
				"tags":        tags[kindIdx],
				"priority":    i % 5,
			},
		}
	}
	return entries
}

// BenchmarkFinder_CacheHit benchmarks cached query performance
func BenchmarkFinder_CacheHit(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			entries := generateEntries(size)
			mockReg := &benchMockRegistry{entries: entries, version: 1}
			finder := NewFinder(mockReg, nil)

			// Warm up cache
			criteria := attrs.Bag{"meta.enabled": true}
			_, _ = finder.Find(criteria)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := finder.Find(criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_CacheMiss benchmarks uncached query performance
func BenchmarkFinder_CacheMiss(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			entries := generateEntries(size)
			mockReg := &benchMockRegistry{entries: entries, version: 1}
			finder := NewFinder(mockReg, nil)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Different query each time to force cache miss
				criteria := attrs.Bag{"meta.port": 8000 + (i % size)}
				_, err := finder.Find(criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_RootFieldMatching benchmarks root field queries
func BenchmarkFinder_RootFieldMatching(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	benchmarks := []struct {
		criteria attrs.Bag
		name     string
	}{
		{attrs.Bag{".kind": "service"}, "kind"},
		{attrs.Bag{".ns": "ns5"}, "namespace"},
		{attrs.Bag{".name": "entry-500"}, "name"},
		{attrs.Bag{".kind": "service", ".ns": "ns5"}, "combined"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := finder.Find(bm.criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_MetadataEquality benchmarks metadata equality queries
func BenchmarkFinder_MetadataEquality(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	benchmarks := []struct {
		criteria attrs.Bag
		name     string
	}{
		{attrs.Bag{"meta.enabled": true}, "boolean"},
		{attrs.Bag{"meta.port": 8500}, "integer"},
		{attrs.Bag{"meta.version": "v5.0.0"}, "string"},
		{attrs.Bag{"meta.enabled": true, "meta.priority": 3}, "combined"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := finder.Find(bm.criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_RegexMatching benchmarks regex pattern queries
func BenchmarkFinder_RegexMatching(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	benchmarks := []struct {
		criteria attrs.Bag
		name     string
	}{
		{attrs.Bag{"~meta.description": ".*service.*"}, "simple"},
		{attrs.Bag{"~meta.version": `^v5\.`}, "version"},
		{attrs.Bag{"~meta.description": ".*service.*", ".kind": "service"}, "complex"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := finder.Find(bm.criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_RegexCaching benchmarks regex compilation caching
func BenchmarkFinder_RegexCaching(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	b.Run("first_use", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Create new finder each time to measure regex compilation cost
			f := NewFinder(mockReg, nil)
			_, err := f.Find(attrs.Bag{"~meta.description": ".*service.*"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("cached", func(b *testing.B) {
		// Warm up regex cache
		_, _ = finder.Find(attrs.Bag{"~meta.description": ".*service.*"})

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := finder.Find(attrs.Bag{"~meta.description": ".*service.*"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkFinder_ContainsMatching benchmarks substring queries
func BenchmarkFinder_ContainsMatching(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	benchmarks := []struct {
		criteria attrs.Bag
		name     string
	}{
		{attrs.Bag{"*meta.description": "service"}, "string"},
		{attrs.Bag{"*meta.tags": "backend"}, "array"},
		{attrs.Bag{"*meta.description": "service", ".kind": "service"}, "combined"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := finder.Find(bm.criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_ArrayMatching benchmarks array field queries
func BenchmarkFinder_ArrayMatching(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	benchmarks := []struct {
		criteria attrs.Bag
		name     string
	}{
		{attrs.Bag{"meta.tags": []string{"api"}}, "single_tag"},
		{attrs.Bag{"meta.tags": []string{"api", "rest"}}, "multiple_tags"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := finder.Find(bm.criteria)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFinder_VersionInvalidation benchmarks cache invalidation on version change
func BenchmarkFinder_VersionInvalidation(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)
	criteria := attrs.Bag{"meta.enabled": true}

	// Warm up cache
	_, _ = finder.Find(criteria)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mockReg.version = uint(i) + 2
		_, err := finder.Find(criteria)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFinder_Fork benchmarks forked finder performance
func BenchmarkFinder_Fork(b *testing.B) {
	mainEntries := generateEntries(1000)
	mainReg := &benchMockRegistry{entries: mainEntries, version: 1}
	mainFinder := NewFinder(mainReg, nil)

	// Warm up regex cache in main finder
	_, _ = mainFinder.Find(attrs.Bag{"~meta.description": ".*service.*"})

	snapshotEntries := generateEntries(500)
	snapshotReg := &benchMockRegistry{entries: snapshotEntries, version: 1}

	b.Run("with_shared_cache", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			forked := Fork(mainFinder, snapshotReg, nil)
			_, err := forked.Find(attrs.Bag{"~meta.description": ".*service.*"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("without_shared_cache", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			newFinder := NewFinder(snapshotReg, nil)
			_, err := newFinder.Find(attrs.Bag{"~meta.description": ".*service.*"})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkFinder_ComplexQuery benchmarks complex multi-criteria queries
func BenchmarkFinder_ComplexQuery(b *testing.B) {
	entries := generateEntries(1000)
	mockReg := &benchMockRegistry{entries: entries, version: 1}
	finder := NewFinder(mockReg, nil)

	criteria := attrs.Bag{
		".kind":             "service",
		"meta.enabled":      true,
		"~meta.description": ".*service.*",
		"*meta.tags":        "backend",
		"^meta.version":     "v5",
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := finder.Find(criteria)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFinder_EmptyCriteria benchmarks queries with no filtering
func BenchmarkFinder_EmptyCriteria(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			entries := generateEntries(size)
			mockReg := &benchMockRegistry{entries: entries, version: 1}
			finder := NewFinder(mockReg, nil)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				results, err := finder.Find(attrs.Bag{})
				if err != nil {
					b.Fatal(err)
				}
				if len(results) != size {
					b.Fatalf("expected %d results, got %d", size, len(results))
				}
			}
		})
	}
}
