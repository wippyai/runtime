// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/queue/drivertest"
	"go.uber.org/zap/zaptest"
)

// TestMemoryDriver_Conformance runs the shared driver conformance suite.
func TestMemoryDriver_Conformance(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)
	drivertest.New(t, driver).Run()
}

// --- Memory-specific tests below (internal state and lifecycle) ---

func TestMemoryDriver_DeclareQueueInternal(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")
	memBag := attrs.NewBag()
	memBag.Set("max_length", 50)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)

	err := driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	driver.mu.RLock()
	q, exists := driver.queues[queueID]
	driver.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, queueID, q.id)
	assert.Equal(t, 50, cap(q.messages))
}

func TestMemoryDriver_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
	require.NoError(t, err)

	_, err = driver.Start(ctx)
	require.NoError(t, err)

	err = driver.Stop(ctx)
	require.NoError(t, err)

	driver.mu.RLock()
	queueCount := len(driver.queues)
	driver.mu.RUnlock()

	assert.Equal(t, 0, queueCount)
}

func TestMemoryDriver_PublishBeforeStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("test"))
	msg.ID = "msg1"

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err, "publish should work before start")
}

func TestMemoryDriver_PublishAfterStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
	require.NoError(t, err)

	_, err = driver.Start(ctx)
	require.NoError(t, err)

	err = driver.Stop(ctx)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("test"))
	msg.ID = "msg1"

	err = driver.Publish(ctx, queueID, msg)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound, "publish should fail after stop")
}

func TestMemoryDriver_StopWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	err := driver.Stop(ctx)
	require.NoError(t, err, "stop without start should not panic")
}
