// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// A queue's Config can change between declarations (codec flip, driver
// option tweak, dead-letter target swap). DeclareQueue previously short-
// circuited on the "already exists" branch and kept the first Config
// forever — so Update had no effect on live drivers until process restart.
//
// The contract: DeclareQueue is idempotent on identity but re-applies the
// latest Config to the stored queue, so subsequent Publish and GetQueueInfo
// use the updated cfg pointer.
func TestDeclareQueue_RedeclarationUpdatesConfig(t *testing.T) {
	ctx := context.Background()
	d := NewDriver(registry.NewID("test", "mem"), zap.NewNop())

	queueID := registry.NewID("app", "orders")

	initial := &queueapi.Config{Codec: "json"}
	require.NoError(t, d.DeclareQueue(ctx, queueID, initial))
	assert.Same(t, initial, d.queues[queueID].cfg,
		"first declaration stores the supplied cfg")

	updated := &queueapi.Config{Codec: "msgpack"}
	require.NoError(t, d.DeclareQueue(ctx, queueID, updated))

	assert.Same(t, updated, d.queues[queueID].cfg,
		"redeclare must swap in the latest cfg pointer")
}

// Swapping the cfg pointer must not blow away in-flight messages — the
// point of redeclare-as-update is to tweak configuration without a full
// teardown. Messages already queued continue to drain.
func TestDeclareQueue_RedeclarationKeepsQueuedMessages(t *testing.T) {
	ctx := context.Background()
	d := NewDriver(registry.NewID("test", "mem"), zap.NewNop())

	queueID := registry.NewID("app", "orders")

	cfg := &queueapi.Config{}
	cfg.DriverOptions = attrs.NewBag()
	cfg.DriverBag("memory").Set(optionMaxLength, 10)

	require.NoError(t, d.DeclareQueue(ctx, queueID, cfg))

	msg := queueapi.NewMessage(nil)
	msg.ID = "in-flight"
	require.NoError(t, d.Publish(ctx, queueID, msg))

	// Redeclare with a different cfg and confirm the backlog survives.
	require.NoError(t, d.DeclareQueue(ctx, queueID, &queueapi.Config{}))

	assert.Equal(t, 1, len(d.queues[queueID].messages),
		"redeclare must not drop already-queued messages")
}
