// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/loader"
	systempayload "github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

func TestManagerRestartIntensityFromYAMLEntry(t *testing.T) {
	for _, tc := range []struct {
		want            *supervisor.RestartIntensity
		name, intensity string
		invalid         bool
	}{
		{name: "omitted"},
		{name: "null", intensity: "      intensity: null\n"},
		{name: "configured", intensity: "      intensity:\n        max_restarts: 3\n        window: 1m\n",
			want: &supervisor.RestartIntensity{MaxRestarts: 3, Window: time.Minute}},
		{name: "zero-count", intensity: "      intensity:\n        max_restarts: 0\n        window: 1m\n", invalid: true},
		{name: "negative-window", intensity: "      intensity:\n        max_restarts: 3\n        window: -1m\n", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dtt := systempayload.NewTranscoder()
			boot.ConfigureTranscoder(ctx, dtt)
			bus := &mockBus{}
			manager := NewManager(bus, dtt, newTestPIDGen(), zap.NewNop())
			id := registry.NewID("test", "intensity")
			processor := loader.NewEntryProcessor(dtt)
			entries, err := processor.ExtractDependenciesToEntries(ctx, payload.NewPayload(
				"version: '1.0'\nnamespace: test\nentries:\n- name: intensity\n  kind: process.service\n  process: test:worker\n  host: test-host\n  lifecycle:\n    restart:\n      initial_delay: 1s\n"+tc.intensity, payload.YAML))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			err = manager.Add(ctx, entries[0])
			if tc.invalid {
				require.Error(t, err)
				require.Empty(t, bus.events, "invalid policy must not register a service")
				return
			}
			require.NoError(t, err)
			require.Len(t, bus.events, 1)
			stored, ok := manager.services.Load(id)
			require.True(t, ok)
			require.Equal(t, tc.want, stored.(*Service).config.Lifecycle.RetryPolicy.Intensity)
		})
	}
}
