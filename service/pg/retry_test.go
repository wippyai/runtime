// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"container/heap"
	"fmt"
	"testing"
	"time"

	pgapi "github.com/wippyai/runtime/api/service/pg"
	"go.uber.org/zap"
)

func TestRetryQueueRespectsCap(t *testing.T) {
	logger := zap.NewNop()
	tel := newTelemetry(nil, nil, nil)
	svc, _, _ := newTestService()
	rq := newRetryQueue(svc, 5, time.Millisecond, time.Second, logger, tel)
	rq.cap = 8

	for i := 0; i < 100; i++ {
		rq.Add(fmt.Sprintf("n-%d", i),
			pgapi.TopicJoin, []string{"g"}, nil, nil)
	}

	rq.mu.Lock()
	defer rq.mu.Unlock()
	if len(rq.entries) > rq.cap {
		t.Fatalf("retry queue exceeded cap: got %d, want <= %d", len(rq.entries), rq.cap)
	}
}

func TestRetryQueueHeapOrder(t *testing.T) {
	logger := zap.NewNop()
	tel := newTelemetry(nil, nil, nil)
	rq := newRetryQueue(nil, 5, time.Millisecond, time.Second, logger, tel)

	now := time.Now()
	rq.entries = []*retryEntry{
		{nextTry: now.Add(50 * time.Millisecond)},
		{nextTry: now.Add(10 * time.Millisecond)},
		{nextTry: now.Add(30 * time.Millisecond)},
	}
	heap.Init((*retryHeap)(&rq.entries))

	first := heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry)
	if !first.nextTry.Equal(now.Add(10 * time.Millisecond)) {
		t.Fatalf("heap pop must return earliest nextTry first")
	}
}
