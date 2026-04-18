// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap/zaptest"
)

// redeclareQueuesLocked used to run every QueueDeclare on a single
// channel. When the first declare raises a channel exception
// (PRECONDITION_FAILED — queue already exists with incompatible args)
// the broker closes that channel, and every subsequent declare on the
// same channel is silently rejected with "channel/connection is not
// open". Queues after the failure in the iteration order are lost on
// reconnect. The fix opens a fresh channel per declare so each queue's
// fate is independent.
func TestAMQPDriver_Redeclare_IndependentChannelsPerQueue(t *testing.T) {
	if testAMQPURL == "" {
		t.Skip("AMQP URL not available (docker or AMQP_URL required)")
	}

	logger := zaptest.NewLogger(t)
	tc := systempayload.NewTranscoder()
	sysjson.Register(tc)
	cfg := &amqpapi.Config{
		URL:               testAMQPURL,
		ReconnectDelay:    50 * time.Millisecond,
		ReconnectMaxDelay: 500 * time.Millisecond,
	}
	driver := NewDriver(registry.ParseID("test:redeclare-channels"), cfg, tc, logger)

	ctx := context.Background()
	_, err := driver.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = driver.Stop(ctx) })

	// Declare one queue with max_length=5 — this will be the conflict
	// queue once we mutate its recorded cfg below.
	tag := time.Now().Format("150405.000")
	conflictID := registry.NewID("test", "redeclare-conflict-"+tag)
	conflictCfg := &queueapi.Config{
		QueueName:     conflictID.Name,
		DriverOptions: attrs.NewBag(),
	}
	conflictCfg.DriverOptions.Set("amqp", attrs.Bag{optionMaxLength: 5})
	require.NoError(t, driver.DeclareQueue(ctx, conflictID, conflictCfg))

	// And a bunch of clean queues. Go map iteration is randomized, so
	// multiple queues maximize the odds that at least one lands after
	// the conflicting one in the redeclare loop — which is the exact
	// position where the pre-fix bug swallows them.
	cleanIDs := make([]registry.ID, 0, 6)
	for i := 0; i < 6; i++ {
		id := registry.NewID("test", fmt.Sprintf("redeclare-clean-%d-%s", i, tag))
		require.NoError(t, driver.DeclareQueue(ctx, id, &queueapi.Config{QueueName: id.Name}))
		cleanIDs = append(cleanIDs, id)
	}

	// Mutate the conflict queue's recorded max_length to 99 so that the
	// redeclare call raises PRECONDITION_FAILED against the existing
	// broker-side queue (which has max_length=5).
	driver.mu.Lock()
	newBag := attrs.NewBag()
	newBag.Set("amqp", attrs.Bag{optionMaxLength: 99})
	driver.queues[conflictID].cfg = &queueapi.Config{
		QueueName:     conflictID.Name,
		DriverOptions: newBag,
	}
	driver.mu.Unlock()

	// Delete the clean queues server-side so the redeclare pass must
	// actually recreate them — otherwise durable queues persist across
	// reconnect and GetQueueInfo would succeed even if redeclare was
	// skipped, masking the shared-channel bug.
	for _, id := range cleanIDs {
		out, derr := exec.CommandContext(ctx, "docker", "exec", testContainer,
			"rabbitmqctl", "delete_queue", id.Name).CombinedOutput()
		if derr != nil {
			t.Skipf("could not delete queue %s: %v\n%s", id.Name, derr, out)
		}
	}

	// Capture the current connection pointer so we know when the
	// watcher's reconnect has swapped it out.
	driver.mu.Lock()
	originalConn := driver.conn
	driver.mu.Unlock()

	// Force a broker-initiated close so the reconnect path (which calls
	// redeclareQueuesLocked) runs.
	out, err := exec.CommandContext(ctx, "docker", "exec", testContainer,
		"rabbitmqctl", "close_all_connections", "test-redeclare-channels").CombinedOutput()
	if err != nil {
		t.Skipf("could not force-close AMQP connections: %v\n%s", err, out)
	}

	require.Eventually(t, func() bool {
		driver.mu.RLock()
		defer driver.mu.RUnlock()
		return driver.conn != nil && driver.conn != originalConn && !driver.conn.IsClosed()
	}, 5*time.Second, 25*time.Millisecond, "driver never reconnected")

	// Each clean queue must still be inspectable — the conflict queue's
	// channel exception must not have poisoned the other redeclare
	// calls. QueueDeclarePassive (which GetQueueInfo uses) errors out
	// if the queue is missing or the channel is dead.
	for _, id := range cleanIDs {
		_, err := driver.GetQueueInfo(ctx, id)
		require.NoErrorf(t, err, "clean queue %s missing after reconnect — redeclare channel was poisoned", id.Name)
	}
}
