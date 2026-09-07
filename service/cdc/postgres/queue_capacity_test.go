// SPDX-License-Identifier: MPL-2.0
package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestDrainedQueueRetainsCapacity(t *testing.T) {
	source := NewSource(SourceOptions{Name: "test"})
	sub := source.Subscribe(config.StreamOptions{Buffer: 4}).(*sourceSubscription)
	defer sub.Close()
	sub.send(context.Background(), config.Change{Op: "insert"}, 10)
	<-sub.Changes()
	require.Eventually(t, func() bool { sub.mu.Lock(); defer sub.mu.Unlock(); return len(sub.queue) == 0 }, time.Second, time.Millisecond)
	sub.mu.Lock()
	capacity := cap(sub.queue)
	sub.mu.Unlock()
	require.Positive(t, capacity, "a drained queue should reuse its allocation")
}
