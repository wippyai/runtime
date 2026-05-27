// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"sync"
	"testing"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	"go.uber.org/zap"
)

// Regression guard: if a future refactor moves q.cfg / q.name reads out
// from under d.mu, `go test -race` catches it here. The test runs the
// real Publish path concurrently with DeclareQueue (which mutates the
// stored cfg pointer on redeclare). Publish with no live connection
// short-circuits cleanly, but must still snapshot cfg / name under the
// lock before returning.
func TestDeclareQueue_RedeclarationIsRaceFree(t *testing.T) {
	driver := &Driver{
		id:     registry.NewID("test", "amqp"),
		logger: zap.NewNop(),
		cfg:    &amqpapi.Config{},
		queues: map[registry.ID]*declaredQueue{},
	}

	queueID := registry.NewID("app", "orders")
	driver.queues[queueID] = &declaredQueue{
		name: "app.orders",
		cfg:  &queueapi.Config{Codec: "json"},
	}

	ctx := context.Background()
	const iterations = 300

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			// Publish with a nil connection short-circuits after the
			// cfg / name snapshot, exactly the reads we need to exercise.
			_ = driver.Publish(ctx, queueID, queueapi.NewMessage(nil))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			next := &queueapi.Config{Codec: "msgpack"}
			driver.mu.Lock()
			if existing, ok := driver.queues[queueID]; ok {
				existing.cfg = next
			}
			driver.mu.Unlock()
		}
	}()

	wg.Wait()
}
