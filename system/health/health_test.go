// SPDX-License-Identifier: MPL-2.0

package health

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func isolateRegistry(t *testing.T) {
	t.Helper()

	mu.Lock()
	previousChecks := checks
	previousEnabled := enabled
	previousDisabled := disabled
	checks = make(map[string]Check)
	enabled = true
	disabled = make(map[string]bool)
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		checks = previousChecks
		enabled = previousEnabled
		disabled = previousDisabled
		mu.Unlock()
	})
}

func TestG01RegisterReplacesCheck(t *testing.T) {
	isolateRegistry(t)

	oldErr := errors.New("old check ran")
	replacementErr := errors.New("replacement check ran")
	var oldCalls atomic.Int32
	var replacementCalls atomic.Int32
	Register("database", func() error {
		oldCalls.Add(1)
		return oldErr
	})
	Register("database", func() error {
		replacementCalls.Add(1)
		return replacementErr
	})

	results := Run()
	if len(results) != 1 {
		t.Fatalf("Run() returned %d results, want 1: %#v", len(results), results)
	}
	if results[0].Name != "database" {
		t.Fatalf("Run()[0].Name = %q, want database", results[0].Name)
	}
	require.Same(t, replacementErr, results[0].Err)
	if got := oldCalls.Load(); got != 0 {
		t.Fatalf("old check calls = %d, want 0", got)
	}
	if got := replacementCalls.Load(); got != 1 {
		t.Fatalf("replacement check calls = %d, want 1", got)
	}
}

func TestG02RegisterNilUnregisters(t *testing.T) {
	isolateRegistry(t)

	var calls atomic.Int32
	Register("cache", func() error {
		calls.Add(1)
		return nil
	})
	Register("cache", nil)

	results := Run()
	if len(results) != 0 {
		t.Fatalf("Run() after unregister = %#v, want no results", results)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("unregistered check calls = %d, want 0", got)
	}
}

func TestG03RunSortsAndPreservesErrors(t *testing.T) {
	isolateRegistry(t)

	alphaErr := errors.New("alpha failed")
	muErr := errors.New("mu failed")
	zetaErr := errors.New("zeta failed")
	Register("zeta", func() error { return zetaErr })
	Register("alpha", func() error { return alphaErr })
	Register("mu", func() error { return muErr })

	results := Run()
	wantNames := []string{"alpha", "mu", "zeta"}
	wantErrors := []error{alphaErr, muErr, zetaErr}
	if len(results) != len(wantNames) {
		t.Fatalf("Run() returned %d results, want %d: %#v", len(results), len(wantNames), results)
	}
	for i := range wantNames {
		if results[i].Name != wantNames[i] {
			t.Fatalf("Run()[%d].Name = %q, want %q; results: %#v", i, results[i].Name, wantNames[i], results)
		}
		require.Same(t, wantErrors[i], results[i].Err)
	}
}

func TestG04DisableAndEnableCheck(t *testing.T) {
	isolateRegistry(t)

	var calls atomic.Int32
	Register("queue", func() error {
		calls.Add(1)
		return nil
	})
	Disable("queue")

	disabledResults := Run()
	if len(disabledResults) != 1 || disabledResults[0].Name != "queue" {
		t.Fatalf("Run() while disabled = %#v, want queue result", disabledResults)
	}
	require.Same(t, errDisabled, disabledResults[0].Err)
	if got := calls.Load(); got != 0 {
		t.Fatalf("disabled check calls = %d, want 0", got)
	}

	Enable("queue")
	enabledResults := Run()
	if len(enabledResults) != 1 || enabledResults[0].Name != "queue" || enabledResults[0].Err != nil {
		t.Fatalf("Run() after Enable = %#v, want successful queue result", enabledResults)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("enabled check calls = %d, want 1", got)
	}
}

func TestG05GlobalDisableAndRestore(t *testing.T) {
	isolateRegistry(t)

	var calls atomic.Int32
	Register("alpha", func() error {
		calls.Add(1)
		return nil
	})
	Register("beta", func() error {
		calls.Add(1)
		return nil
	})
	SetEnabled(false)

	disabledResults := Run()
	if len(disabledResults) != 0 {
		t.Fatalf("Run() while globally disabled = %#v, want no results", disabledResults)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("globally disabled check calls = %d, want 0", got)
	}

	SetEnabled(true)
	restoredResults := Run()
	if len(restoredResults) != 2 || restoredResults[0].Name != "alpha" || restoredResults[1].Name != "beta" {
		t.Fatalf("Run() after restoring global state = %#v, want alpha then beta", restoredResults)
	}
	if restoredResults[0].Err != nil || restoredResults[1].Err != nil {
		t.Fatalf("Run() after restoring global state returned errors: %#v", restoredResults)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("restored check calls = %d, want 2", got)
	}
}

func TestG06PanicBecomesErrorAndContinues(t *testing.T) {
	isolateRegistry(t)

	var continuationCalls atomic.Int32
	Register("alpha-panic", func() error {
		panic("broken check")
	})
	Register("beta-continues", func() error {
		continuationCalls.Add(1)
		return nil
	})

	results := Run()
	if len(results) != 2 {
		t.Fatalf("Run() returned %d results, want 2: %#v", len(results), results)
	}
	if results[0].Name != "alpha-panic" {
		t.Fatalf("panic result name = %q, want alpha-panic", results[0].Name)
	}
	require.Same(t, errCheckPanic, results[0].Err)
	if results[1].Name != "beta-continues" || results[1].Err != nil {
		t.Fatalf("continuation result = %#v, want successful beta-continues", results[1])
	}
	if got := continuationCalls.Load(); got != 1 {
		t.Fatalf("continuation check calls = %d, want 1", got)
	}
}

func TestG07ConcurrentHealthSnapshots(t *testing.T) {
	isolateRegistry(t)

	firstVersionErr := errors.New("first version")
	secondVersionErr := errors.New("second version")
	Register("alpha", func() error { return nil })
	Register("bravo", func() error { return firstVersionErr })
	Register("charlie", func() error { return nil })

	const iterations = 100
	start := make(chan struct{})
	snapshots := make(chan []Result, iterations)
	var workers sync.WaitGroup
	workers.Add(4)

	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				Register("bravo", func() error { return firstVersionErr })
			} else {
				Register("bravo", func() error { return secondVersionErr })
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < iterations; i++ {
			Disable("bravo")
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < iterations; i++ {
			Enable("bravo")
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < iterations; i++ {
			snapshots <- Run()
		}
	}()

	close(start)
	workers.Wait()
	close(snapshots)

	seen := 0
	for results := range snapshots {
		seen++
		if len(results) != 3 {
			t.Fatalf("concurrent Run() returned %d results, want 3: %#v", len(results), results)
		}
		if results[0].Name != "alpha" || results[1].Name != "bravo" || results[2].Name != "charlie" {
			t.Fatalf("concurrent Run() order = [%q %q %q], want [alpha bravo charlie]", results[0].Name, results[1].Name, results[2].Name)
		}
		if results[0].Err != nil || results[2].Err != nil {
			t.Fatalf("stable checks returned errors: %#v", results)
		}
		bravoErr := results[1].Err
		switch bravoErr.Error() {
		case firstVersionErr.Error():
			require.Same(t, firstVersionErr, bravoErr)
		case secondVersionErr.Error():
			require.Same(t, secondVersionErr, bravoErr)
		case errDisabled.Error():
			require.Same(t, errDisabled, bravoErr)
		default:
			t.Fatalf("bravo error = %v, want a registered version error or errDisabled", bravoErr)
		}
	}
	if seen != iterations {
		t.Fatalf("collected %d snapshots, want %d", seen, iterations)
	}
}
