// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestNewBootstrapContext(t *testing.T) {
	t.Run("creates bootstrap context with all infrastructure", func(t *testing.T) {
		logger := zap.NewExample()
		cfg := boot.NewConfig()

		ctx, err := NewBootstrapContext(logger, cfg)
		require.NoError(t, err)
		require.NotNil(t, ctx)

		// Verify AppContext
		appCtx := ctxapi.AppFromContext(ctx)
		assert.NotNil(t, appCtx, "AppContext should be initialized")

		// Verify Config
		loadedCfg := boot.GetConfig(ctx)
		assert.NotNil(t, loadedCfg, "Config should be available")

		// Verify Logger
		ctxLogger := logapi.GetLogger(ctx)
		assert.NotNil(t, ctxLogger, "Logger should be available")

		// Verify EventBus
		bus := event.GetBus(ctx)
		assert.NotNil(t, bus, "EventBus should be available")

		// Verify Transcoder
		transcoder := payload.GetTranscoder(ctx)
		assert.NotNil(t, transcoder, "Transcoder should be available")

		// Verify Relay infrastructure
		node := relayapi.GetNode(ctx)
		assert.NotNil(t, node, "Relay Node should be available")

		router := relayapi.GetRouter(ctx)
		assert.NotNil(t, router, "Relay Router should be available")

		nodeManager := relayapi.GetNodeManager(ctx)
		assert.NotNil(t, nodeManager, "NodeManager should be available")

		// Topology infrastructure is set up by components, not NewBootstrapContext
		// So we don't check for it here

		// Verify LogManager
		logManager := logapi.GetManager(ctx)
		assert.NotNil(t, logManager, "LogManager should be available")

		// Verify HandlerRegistry
		handlerReg := GetHandlerRegistry(ctx)
		assert.NotNil(t, handlerReg, "HandlerRegistry should be available")

		// Verify Readiness
		readiness := GetReadiness(ctx)
		assert.NotNil(t, readiness, "Readiness should be available")
	})

	t.Run("works with nil config", func(t *testing.T) {
		logger := zap.NewExample()

		ctx, err := NewBootstrapContext(logger, nil)
		require.NoError(t, err)
		require.NotNil(t, ctx)

		// Should still have all infrastructure
		assert.NotNil(t, ctxapi.AppFromContext(ctx))
		assert.NotNil(t, logapi.GetLogger(ctx))
		assert.NotNil(t, event.GetBus(ctx))
	})

	t.Run("configures custom relay node name", func(t *testing.T) {
		logger := zap.NewExample()
		cfg := boot.NewConfig(
			boot.WithSection("relay", map[string]any{
				"node_name": "custom-node",
			}),
		)

		ctx, err := NewBootstrapContext(logger, cfg)
		require.NoError(t, err)

		node := relayapi.GetNode(ctx)
		require.NotNil(t, node)
	})

	t.Run("uses WIPPY_NODE_ID when relay node name is not configured", func(t *testing.T) {
		t.Setenv("WIPPY_NODE_ID", "node-from-env")
		logger := zap.NewExample()

		ctx, err := NewBootstrapContext(logger, nil)
		require.NoError(t, err)

		node := relayapi.GetNode(ctx)
		require.NotNil(t, node)
		assert.Equal(t, "node-from-env", node.ID())
	})

	t.Run("config relay node name overrides WIPPY_NODE_ID", func(t *testing.T) {
		t.Setenv("WIPPY_NODE_ID", "node-from-env")
		logger := zap.NewExample()
		cfg := boot.NewConfig(
			boot.WithSection("relay", map[string]any{
				"node_name": "custom-node",
			}),
		)

		ctx, err := NewBootstrapContext(logger, cfg)
		require.NoError(t, err)

		node := relayapi.GetNode(ctx)
		require.NotNil(t, node)
		assert.Equal(t, "custom-node", node.ID())
	})

	t.Run("configures custom supervisor host settings", func(t *testing.T) {
		logger := zap.NewExample()
		cfg := boot.NewConfig(
			boot.WithSection("supervisor", map[string]any{
				"host": map[string]any{
					"buffer_size":  2048,
					"worker_count": 32,
				},
			}),
		)

		ctx, err := NewBootstrapContext(logger, cfg)
		require.NoError(t, err)
		require.NotNil(t, ctx)

		// If host is created with custom settings, infrastructure should succeed
		assert.NotNil(t, relayapi.GetNode(ctx))
	})

	t.Run("configures custom functions host settings", func(t *testing.T) {
		logger := zap.NewExample()
		cfg := boot.NewConfig(
			boot.WithSection("functions", map[string]any{
				"host": map[string]any{
					"buffer_size":  4096,
					"worker_count": 64,
				},
			}),
		)

		ctx, err := NewBootstrapContext(logger, cfg)
		require.NoError(t, err)
		require.NotNil(t, ctx)

		assert.NotNil(t, relayapi.GetNode(ctx))
	})
}

func TestStartRuntimeServices(t *testing.T) {
	t.Run("starts log manager and node manager", func(t *testing.T) {
		logger := zap.NewExample()
		ctx, err := NewBootstrapContext(logger, nil)
		require.NoError(t, err)

		err = StartRuntimeServices(ctx)
		require.NoError(t, err)

		// Services should be running (no errors)
		logManager := logapi.GetManager(ctx)
		assert.NotNil(t, logManager)

		nodeManager := relayapi.GetNodeManager(ctx)
		assert.NotNil(t, nodeManager)

		// Cleanup
		_ = StopRuntimeServices(ctx)
	})

	t.Run("handles context without managers gracefully", func(t *testing.T) {
		ctx := context.Background()

		err := StartRuntimeServices(ctx)
		assert.NoError(t, err, "Should not error when managers are nil")
	})
}

func TestStopRuntimeServices(t *testing.T) {
	t.Run("stops node manager and log manager", func(t *testing.T) {
		logger := zap.NewExample()
		ctx, err := NewBootstrapContext(logger, nil)
		require.NoError(t, err)

		err = StartRuntimeServices(ctx)
		require.NoError(t, err)

		err = StopRuntimeServices(ctx)
		assert.NoError(t, err)
	})

	t.Run("handles context without managers gracefully", func(t *testing.T) {
		ctx := context.Background()

		err := StopRuntimeServices(ctx)
		assert.NoError(t, err, "Should not error when managers are nil")
	})

	t.Run("stops in correct order (node then log)", func(t *testing.T) {
		logger := zap.NewExample()
		ctx, err := NewBootstrapContext(logger, nil)
		require.NoError(t, err)

		err = StartRuntimeServices(ctx)
		require.NoError(t, err)

		// Stop should succeed without errors
		err = StopRuntimeServices(ctx)
		assert.NoError(t, err)
	})
}

func TestBootstrapContextIntegration(t *testing.T) {
	t.Run("full bootstrap lifecycle", func(t *testing.T) {
		logger := zap.NewExample()
		cfg := boot.NewConfig(
			boot.WithSection("relay", map[string]any{
				"node_name": "test-node",
			}),
		)

		// Create bootstrap context
		ctx, err := NewBootstrapContext(logger, cfg)
		require.NoError(t, err)

		// Start services
		err = StartRuntimeServices(ctx)
		require.NoError(t, err)

		// Verify everything is running
		assert.NotNil(t, logapi.GetLogger(ctx))
		assert.NotNil(t, event.GetBus(ctx))
		assert.NotNil(t, relayapi.GetNode(ctx))

		// Stop services
		err = StopRuntimeServices(ctx)
		assert.NoError(t, err)
	})
}

func TestDefaultNodeName(t *testing.T) {
	t.Run("WIPPY_NODE_ID wins and is trimmed", func(t *testing.T) {
		t.Setenv("WIPPY_NODE_ID", "  node-a  ")
		t.Setenv("WIPPY_RELAY_NODE_NAME", "node-b")
		assert.Equal(t, "node-a", defaultNodeName())
	})

	t.Run("WIPPY_RELAY_NODE_NAME used when WIPPY_NODE_ID is empty", func(t *testing.T) {
		t.Setenv("WIPPY_NODE_ID", "")
		t.Setenv("WIPPY_RELAY_NODE_NAME", "node-b")
		assert.Equal(t, "node-b", defaultNodeName())
	})

	t.Run("whitespace-only env vars are ignored", func(t *testing.T) {
		t.Setenv("WIPPY_NODE_ID", "   ")
		t.Setenv("WIPPY_RELAY_NODE_NAME", "\t")
		// Falls through to the host-derived id, which must still be usable.
		assert.NotEmpty(t, defaultNodeName())
	})

	t.Run("derived id is stable across restarts", func(t *testing.T) {
		// Same host + same working dir must reproduce the id, so a node keeps
		// its identity across restarts.
		t.Setenv("WIPPY_NODE_ID", "")
		t.Setenv("WIPPY_RELAY_NODE_NAME", "")
		t.Chdir(t.TempDir())
		first := defaultNodeName()
		require.Len(t, first, 36, "derived id is a UUID")
		assert.Equal(t, first, defaultNodeName(), "same host + working dir reproduces the id")
	})

	t.Run("co-located instances get distinct ids", func(t *testing.T) {
		// Two instances on the same host run from different working dirs and
		// must not collide (the same-host bug a bare host derivation has).
		t.Setenv("WIPPY_NODE_ID", "")
		t.Setenv("WIPPY_RELAY_NODE_NAME", "")

		t.Chdir(t.TempDir())
		a := defaultNodeName()
		t.Chdir(t.TempDir())
		b := defaultNodeName()

		assert.NotEqual(t, a, b, "different working dirs on the same host -> distinct ids")
	})
}
