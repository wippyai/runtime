// SPDX-License-Identifier: MPL-2.0

package eventual_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	systemkv "github.com/wippyai/runtime/system/kv"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

func TestStrongAndEventualKeepIndependentBindings(t *testing.T) {
	for _, strongFirst := range []bool{false, true} {
		name := "eventual-first"
		if strongFirst {
			name = "strong-first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			engine := systemkv.NewService("registry", nil)
			_, err := engine.Start(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = engine.Stop(context.Background()) })
			strong := kvbacked.NewService(engine, "node-A", nil, nil)
			strong.ConfigureStrong(kvbacked.StrongDeps{
				IsLeader: func() bool { return true },
				Members:  func() ([]pid.NodeID, error) { return []pid.NodeID{"node-A"}, nil },
			})
			require.NoError(t, strong.StartReconciler(ctx))
			eventualReg := eventual.NewService(eventual.Config{LocalNodeID: "node-A"})
			strongPID := pid.PID{Node: "node-A", Host: "process", UniqID: "strong"}
			eventualPID := pid.PID{Node: "node-A", Host: "process", UniqID: "eventual"}
			registerStrong := func() {
				_, err := strong.RegisterScope(ctx, "shared", strongPID, globalapi.Strong)
				require.NoError(t, err)
			}
			if strongFirst {
				registerStrong()
			}
			_, err = eventualReg.Register("shared", eventualPID)
			require.NoError(t, err)
			if !strongFirst {
				registerStrong()
			}
			strongResult, err := strong.Lookup(ctx, "shared")
			require.NoError(t, err)
			require.True(t, strongResult.Found)
			require.True(t, strongPID.Equal(strongResult.PID))
			eventualResult, err := eventualReg.Lookup(ctx, "shared")
			require.NoError(t, err)
			require.True(t, eventualResult.Found)
			require.Equal(t, eventualPID, eventualResult.PID)

			// EVENTUAL keeps admitting and resolving names after the CP service
			// stops; neither readiness nor authority reads belong to its path.
			cancel()
			require.NoError(t, engine.Stop(context.Background()))
			_, err = eventualReg.Register("available", eventualPID)
			require.NoError(t, err)
			eventualResult, err = eventualReg.Lookup(context.Background(), "shared")
			require.NoError(t, err)
			require.True(t, eventualResult.Found)
			require.Equal(t, eventualPID, eventualResult.PID)
		})
	}
}
