// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestParticipantNativeStartupRecovery(t *testing.T) {
	for _, mode := range []string{"retired", "active", "same-incarnation", "ordinary-start"} {
		t.Run(mode, func(t *testing.T) {
			inventory, engine := newParticipantTestInventory(t, 4)
			ctx := context.Background()
			require.NoError(t, inventory.enroll(ctx, "member", "previous"))
			if mode != "active" {
				require.NoError(t, inventory.retire(ctx, "member", "previous"))
			}
			incarnation := "successor"
			if mode == "same-incarnation" {
				incarnation = "previous"
			}
			service := NewService(engine, "member", nil, nil)
			service.ConfigureStrong(StrongDeps{Incarnation: incarnation})
			require.NoError(t, service.ConfigureParticipation(4))
			mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
			config := ParticipantEndpointConfig{MaxEntries: 8, MaxValueBytes: 8192, MaxWireBytes: 16384, MaxConcurrentRequests: 2, MaxRedirects: 2, RequestTimeout: time.Second, RefreshInterval: time.Hour}
			endpoint, err := NewParticipantEndpoint(ctx, service, mesh, func(context.Context) (pid.NodeID, error) { return "member", nil }, config)
			require.NoError(t, err)
			mesh.nodes["member"] = endpoint
			t.Cleanup(func() { require.NoError(t, endpoint.Stop(ctx)) })
			if mode == "ordinary-start" {
				err = endpoint.Start(ctx)
			} else {
				err = endpoint.StartAfterRetirement(ctx)
			}
			if mode == "retired" {
				require.NoError(t, err)
				require.True(t, service.NameReady())
				active, _, err := inventory.readSnapshot()
				require.NoError(t, err)
				require.Equal(t, "successor", active["member"])
				// Reusing a previously started endpoint must never execute a fresh
				// predecessor read/replacement after a later retirement cycle.
				require.NoError(t, inventory.retire(ctx, "member", "successor"))
				require.ErrorContains(t, endpoint.StartAfterRetirement(ctx), "already started")
				retired, _, err := inventory.retiredRecord()
				require.NoError(t, err)
				require.Equal(t, "successor", retired["member"])
			} else {
				require.Error(t, err)
				require.False(t, service.NameReady())
				if mode == "active" {
					require.ErrorIs(t, err, ErrParticipantIncarnationConflict)
				}
				if mode == "ordinary-start" {
					require.ErrorIs(t, err, ErrParticipantRetired)
				}
			}
		})
	}
}
