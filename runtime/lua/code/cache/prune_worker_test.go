// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPruneWorkerCoalescesConcurrentRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	w := pruneWorker{prune: func() error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	}}
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		w.wait()
	})
	w.request()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("prune worker did not start")
	}
	var requests sync.WaitGroup
	for range 32 {
		requests.Go(w.request)
	}
	requests.Wait()
	require.Equal(t, int32(1), calls.Load(), "requests started overlapping cleanup passes")
	unblock()
	w.wait()
	require.Equal(t, int32(2), calls.Load(), "requests during a scan should coalesce into one further pass")
}
