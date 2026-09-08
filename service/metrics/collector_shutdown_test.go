// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
)

// Concurrent producers and shutdown must account for every metric: either it
// was admitted and flushed, or it was explicitly counted as dropped. In
// particular an atomic closed check alone cannot protect channel send/close.
func TestCollectorConcurrentCloseAccountsForAllRecords(t *testing.T) {
	for range 100 {
		cfg := apicfg.Config{}
		cfg.Buffer.Size = 64
		c := NewCollector(cfg).(*collector)
		exporter := &mockExporter{}
		require.NoError(t, c.RegisterExporter(exporter))
		start := make(chan struct{})
		var workers sync.WaitGroup
		const producers = 8
		const records = 1000
		for range producers {
			workers.Go(func() {
				<-start
				for range records {
					c.CounterInc("shutdown", nil)
				}
			})
		}
		stopped := make(chan error, 1)
		go func() { <-start; stopped <- c.Close() }()
		close(start)
		workers.Wait()
		require.NoError(t, <-stopped)
		require.EqualValues(t, producers*records, uint64(exporter.count())+c.Dropped())
		before := c.Dropped()
		c.CounterInc("after-close", nil)
		require.Equal(t, before+1, c.Dropped())
	}
}
