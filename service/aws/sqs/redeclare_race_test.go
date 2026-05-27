// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"sync"
	"testing"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	"go.uber.org/zap"
)

// Regression guard: if a future refactor moves a q.cfg / q.url read out
// from under d.mu, `go test -race` catches it here. The test runs real
// Publish and GetQueueInfo paths concurrently with DeclareQueue, which
// mutates the stored cfg pointer. Both helpers short-circuit cleanly
// when the driver has no client, but must still snapshot cfg and url
// under the lock before returning.
func TestDeclareQueue_RedeclarationIsRaceFree(t *testing.T) {
	driver := &Driver{
		id:     registry.NewID("test", "sqs"),
		logger: zap.NewNop(),
		cfg:    &sqsapi.Config{},
		queues: map[registry.ID]*declaredQueue{},
	}

	queueID := registry.NewID("app", "orders")
	driver.queues[queueID] = &declaredQueue{
		url: "http://local/queues/app-orders",
		cfg: &queueapi.Config{Codec: "json"},
	}

	ctx := context.Background()
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			// Publish with a nil client short-circuits after the snapshot,
			// so the cfg / url reads are exactly what we want to exercise.
			_ = driver.Publish(ctx, queueID, queueapi.NewMessage(nil))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _ = driver.GetQueueInfo(ctx, queueID)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			next := &queueapi.Config{Codec: "msgpack"}
			_ = driver.DeclareQueue(ctx, queueID, next)
		}
	}()

	wg.Wait()
}
