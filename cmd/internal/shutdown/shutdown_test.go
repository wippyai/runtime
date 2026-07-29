// SPDX-License-Identifier: MPL-2.0

package shutdown

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	bootloader "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
)

func TestB08ShutdownUsesFreshStopContext(t *testing.T) {
	var stopErr error
	var stopDeadline time.Time
	var stopBounded bool
	component := bootapi.New(bootapi.P{
		Name: "context-recorder",
		Stop: func(ctx context.Context) error {
			stopErr = ctx.Err()
			stopDeadline, stopBounded = ctx.Deadline()
			return nil
		},
	})
	loader, err := bootloader.NewLoader(component)
	require.NoError(t, err)
	ctx := contextapi.WithAppContext(context.Background(), contextapi.NewAppContext())
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	ctx, err = loader.Load(ctx)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	Perform(canceled, loader, zap.NewNop(), true)
	require.NoError(t, stopErr, "stop context inherited runtime cancellation")
	require.True(t, stopBounded, "stop context must be bounded")
	require.WithinDuration(t, time.Now().Add(30*time.Second), stopDeadline, time.Second)
}
