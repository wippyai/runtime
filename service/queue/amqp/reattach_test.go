// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap/zaptest"
)

// After the TCP connection drops and the watcher establishes a fresh one,
// the amqp091 deliveries channel that each Attach goroutine was reading
// from is closed — broker-side the consumer is gone. The existing code
// exits the goroutine on channel close, so the caller silently stops
// receiving messages even though Publish is back up and the queue was
// re-declared on the new connection. The driver must re-open a channel
// and re-call Consume for every active attachment after reconnect.
func TestAMQPDriver_Attach_ReattachesAfterReconnect(t *testing.T) {
	if testAMQPURL == "" {
		t.Skip("AMQP URL not available (docker or AMQP_URL required)")
	}

	logger := zaptest.NewLogger(t)
	cfg := &amqpapi.Config{
		URL:               testAMQPURL,
		ReconnectDelay:    50 * time.Millisecond,
		ReconnectMaxDelay: 500 * time.Millisecond,
	}
	tc := systempayload.NewTranscoder()
	sysjson.Register(tc)
	driver := NewDriver(registry.ParseID("test:amqp-reattach"), cfg, tc, logger)

	ctx := context.Background()
	_, err := driver.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = driver.Stop(ctx) })

	queueID := registry.NewID("test", "reattach-"+time.Now().Format("150405.000"))
	require.NoError(t, driver.DeclareQueue(ctx, queueID, &queueapi.Config{}))

	deliveries := make(chan *queueapi.Delivery, 16)
	cancelAttach, err := driver.Attach(ctx, queueID, &queueapi.ConsumerOptions{AutoAck: true}, deliveries)
	require.NoError(t, err)
	t.Cleanup(cancelAttach)

	// Baseline delivery on the original connection.
	baseMsg := queueapi.AcquireMessage(payload.New("hi"))
	baseMsg.ID = "baseline"
	require.NoError(t, driver.Publish(ctx, queueID, baseMsg))

	select {
	case d := <-deliveries:
		require.Equal(t, "baseline", d.Message.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("baseline delivery never arrived")
	}

	// Snapshot the current connection before we force-kill it — we
	// recognize the reconnect completed when d.conn swaps to a new
	// pointer.
	driver.mu.Lock()
	originalConn := driver.conn
	driver.mu.Unlock()

	// Force the broker to close all client connections abnormally. A
	// client-side Close() would be treated as a graceful shutdown by
	// the watcher and no reconnect would be triggered; this matches a
	// real broker-initiated drop (network failure, server restart).
	out, err := exec.CommandContext(ctx, "docker", "exec", testContainer,
		"rabbitmqctl", "close_all_connections", "test-reattach").CombinedOutput()
	if err != nil {
		t.Skipf("could not force-close AMQP connections via rabbitmqctl: %v\n%s", err, out)
	}

	// Wait for the watcher to install a fresh, open connection.
	require.Eventually(t, func() bool {
		driver.mu.RLock()
		defer driver.mu.RUnlock()
		return driver.conn != nil && driver.conn != originalConn && !driver.conn.IsClosed()
	}, 5*time.Second, 25*time.Millisecond, "driver never reconnected")

	// Drain anything that lingered during the reconnect window; the
	// baseline-pool-released message is impossible to get here because
	// amqp091 stops pushing on the dead channel, but any probe from
	// future retries would show up and could mask the real assertion.
	drainDeliveries(deliveries)

	// Publish on the fresh connection. This must succeed — redeclare
	// restored the queue.
	require.Eventually(t, func() bool {
		m := queueapi.AcquireMessage(payload.New("hello-again"))
		m.ID = "post-reconnect"
		return driver.Publish(ctx, queueID, m) == nil
	}, 5*time.Second, 50*time.Millisecond, "publish never succeeded after reconnect")

	// The real assertion: the same Attach caller keeps receiving. If the
	// consumer goroutine exited when the amqp091 deliveries channel
	// closed and nothing re-attached it, this times out.
	select {
	case d := <-deliveries:
		require.Equal(t, "post-reconnect", d.Message.ID,
			"consumer must resume delivery on the new connection")
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery after reconnect — consumer was not re-attached")
	}
}

func drainDeliveries(ch <-chan *queueapi.Delivery) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
