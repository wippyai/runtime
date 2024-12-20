package path

import (
	"github.com/ponyruntime/pony/api/registry"
	"sort"
	"strings"
)

func SortPaths(paths []registry.ID) []registry.ID {
	// 1. Determine Levels and Bucket
	buckets := make(map[int][]registry.ID)
	for _, path := range paths {
		level := len(strings.Split(string(path), "."))
		buckets[level] = append(buckets[level], path)
	}

	// 2. Sort Within Buckets
	for _, paths := range buckets {
		sort.Slice(paths, func(i, j int) bool {
			return string(paths[i]) < string(paths[j])
		})
	}

	// 3. Glue Buckets Together
	var sortedPaths []registry.ID
	var levels []int
	for level := range buckets {
		levels = append(levels, level)
	}

	sort.Ints(levels)

	for _, level := range levels {
		if level == 1 {

			for _, path := range buckets[level] {
				if !strings.Contains(string(path), ".") {
					sortedPaths = append(sortedPaths, path)
				} else {
					sortedPaths = append(sortedPaths, path)
				}
			}
		} else {

			for _, path := range buckets[level] {
				sortedPaths = append(sortedPaths, path)
			}
		}
	}

	return sortedPaths
}
