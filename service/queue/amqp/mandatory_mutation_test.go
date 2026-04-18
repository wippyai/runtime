// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

// The AMQP driver's per-publish mandatory-flag extraction used to mutate
// the caller's msg.Headers bag (delete the "amqp.mandatory" key). That's
// a surprising side-effect: a caller that reuses a header template, or
// retries a failed publish, would see the flag silently dropped on the
// second call. The driver must read the flag without mutating the
// caller-owned bag.
func TestAMQPDriver_Publish_DoesNotMutateCallerHeaders(t *testing.T) {
	driver := setupDriver(t)
	ctx := context.Background()

	queueID := registry.NewID("test", "mandatory-mutation-"+time.Now().Format("150405.000"))
	require.NoError(t, driver.DeclareQueue(ctx, queueID, &queueapi.Config{
		QueueName: queueID.Name,
	}))

	msg := queueapi.AcquireMessage(payload.New("hi"))
	msg.ID = "mandatory-mutation"
	msg.Headers.Set(publishMandatory, true)
	msg.Headers.Set("correlation_id", "req-abc")

	// Snapshot caller-visible state before publish.
	beforeMandatory, hadMandatory := msg.Headers[publishMandatory]
	require.True(t, hadMandatory)
	require.Equal(t, true, beforeMandatory)

	require.NoError(t, driver.Publish(ctx, queueID, msg))

	// Post-publish, the caller's bag must still contain the mandatory
	// flag — the driver owns a derived (merged) copy for its own use.
	afterMandatory, stillHas := msg.Headers[publishMandatory]
	require.True(t, stillHas,
		"driver must not strip amqp.mandatory from the caller's msg.Headers")
	require.Equal(t, beforeMandatory, afterMandatory,
		"driver must not rewrite the mandatory flag value in the caller's headers")

	// Other headers must also be untouched.
	cid, hasCID := msg.Headers["correlation_id"]
	require.True(t, hasCID)
	require.Equal(t, "req-abc", cid)
}
