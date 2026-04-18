// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	"go.uber.org/zap"
)

// Redeclaring a queue that already exists in the driver map must swap in
// the latest cfg pointer rather than dropping the update. The contract
// mirrors the memory and AMQP drivers: DeclareQueue is idempotent on
// identity but re-applies the latest Config so later Publish/GetQueueInfo
// see the new codec / delivery / driver-options.
func TestDeclareQueue_RedeclarationUpdatesConfig(t *testing.T) {
	driver := &Driver{
		id:     registry.NewID("test", "sqs"),
		logger: zap.NewNop(),
		cfg:    &sqsapi.Config{},
		queues: map[registry.ID]*declaredQueue{},
	}

	queueID := registry.NewID("app", "orders")

	initial := &queueapi.Config{Codec: "json"}
	driver.queues[queueID] = &declaredQueue{
		url: "http://local/queues/app-orders",
		cfg: initial,
	}

	updated := &queueapi.Config{Codec: "msgpack"}
	require.NoError(t, driver.DeclareQueue(context.Background(), queueID, updated))

	assert.Same(t, updated, driver.queues[queueID].cfg,
		"redeclare must swap in the latest cfg pointer")
	assert.Equal(t, "http://local/queues/app-orders", driver.queues[queueID].url,
		"redeclare must preserve the existing SQS URL; no re-resolution")
}
