// SPDX-License-Identifier: MPL-2.0

package wippy

import (
	"sync"
	"testing"
)

func TestR12AsyncTokenLifecycle(t *testing.T) {
	store := NewAsyncValueStore()
	first := store.Put("first")
	second := store.Put("second")
	if first == 0 || second == 0 || first == second {
		t.Fatalf("initial tokens = (%d, %d), want distinct non-zero values", first, second)
	}

	got, ok := store.Take(first)
	if !ok || got != "first" {
		t.Fatalf("first Take = (%#v, %v), want (first, true)", got, ok)
	}
	if got, ok := store.Take(first); ok || got != nil {
		t.Fatalf("second Take = (%#v, %v), want (nil, false)", got, ok)
	}

	store.Reset()
	if got, ok := store.Take(second); ok || got != nil {
		t.Fatalf("Take of pre-reset token = (%#v, %v), want (nil, false)", got, ok)
	}
	third := store.Put("third")
	if third == first || third == second {
		t.Fatalf("post-reset token = %d, want unique from prior tokens %d and %d", third, first, second)
	}
	if got, ok := store.Take(third); !ok || got != "third" {
		t.Fatalf("post-reset Take = (%#v, %v), want (third, true)", got, ok)
	}
}

func TestR13AsyncTokenConcurrentSingleUse(t *testing.T) {
	const workers = 32
	type tokenValue struct {
		token uint64
		value int
	}
	type takenValue struct {
		value any
		ok    bool
	}

	store := NewAsyncValueStore()
	startPut := make(chan struct{})
	putResults := make(chan tokenValue, workers)
	var puts sync.WaitGroup
	puts.Add(workers)
	for value := 0; value < workers; value++ {
		go func() {
			defer puts.Done()
			<-startPut
			putResults <- tokenValue{token: store.Put(value), value: value}
		}()
	}
	close(startPut)
	puts.Wait()
	close(putResults)

	tokens := make([]tokenValue, 0, workers)
	seenTokens := make(map[uint64]bool, workers)
	for result := range putResults {
		if result.token == 0 || seenTokens[result.token] {
			t.Fatalf("Put token %d is zero or duplicated", result.token)
		}
		seenTokens[result.token] = true
		tokens = append(tokens, result)
	}

	startTake := make(chan struct{})
	takeResults := make(chan takenValue, workers)
	var takes sync.WaitGroup
	takes.Add(workers)
	for _, result := range tokens {
		go func() {
			defer takes.Done()
			<-startTake
			value, ok := store.Take(result.token)
			takeResults <- takenValue{value: value, ok: ok}
		}()
	}
	close(startTake)
	takes.Wait()
	close(takeResults)

	seenValues := make([]bool, workers)
	for result := range takeResults {
		value, ok := result.value.(int)
		if !result.ok || !ok || value < 0 || value >= workers || seenValues[value] {
			t.Fatalf("Take result = (%#v, %v), want each fixed value exactly once", result.value, result.ok)
		}
		seenValues[value] = true
	}
	for value, seen := range seenValues {
		if !seen {
			t.Fatalf("value %d was not recovered", value)
		}
	}
}
