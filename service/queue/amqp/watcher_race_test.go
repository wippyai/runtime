// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap/zaptest"
)

// The reconnect watcher must exit cleanly when the driver is Stop'd
// while it's parked in the inner reconnect-backoff loop. Pre-fix the
// backoff re-read d.ctx unlocked; post-fix it only reads the ctx
// snapshot taken under the outer iteration's read lock. Under -race
// this test exercises the shutdown path that used to straddle the
// lock boundary.
func TestAMQPDriver_Watcher_ShutdownDuringReconnectBackoff(t *testing.T) {
	if testAMQPURL == "" {
		t.Skip("AMQP URL not available (docker or AMQP_URL required)")
	}

	logger := zaptest.NewLogger(t)
	tc := systempayload.NewTranscoder()
	sysjson.Register(tc)

	// Keep the backoff window long enough that the watcher is parked in
	// the inner reconnect loop when Stop/Start hits; a tight cycle of
	// dial-success would park it in the outer NotifyClose select
	// instead, which is not the path we're testing.
	cfg := &amqpapi.Config{
		URL:               testAMQPURL,
		ReconnectDelay:    200 * time.Millisecond,
		ReconnectMaxDelay: 500 * time.Millisecond,
	}

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			driver := NewDriver(registry.NewID("test", "watcher-race"), cfg, tc, logger)
			_, err := driver.Start(ctx)
			require.NoError(t, err)

			// Force the watcher into its reconnect backoff by having
			// the broker drop the connection abnormally.
			_ = exec.CommandContext(ctx, "docker", "exec", testContainer,
				"rabbitmqctl", "close_all_connections", "watcher-race").Run()

			// Short sleep so the watcher enters the backoff and starts
			// exercising the ctx read paths.
			time.Sleep(50 * time.Millisecond)

			require.NoError(t, driver.Stop(ctx))
		}(i)
	}

	wg.Wait()
}
