// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestServiceSendFailurePreservesPackageForRetry(t *testing.T) {
	for _, failure := range []string{"encoding", "queue"} {
		t.Run(failure, func(t *testing.T) {
			service, manager, codec, _, ctx, cancel := setupService(t)
			defer cancel()
			require.NoError(t, service.Start(ctx))
			defer service.Stop()
			cause := errors.New("injected refusal")
			if failure == "encoding" {
				codec.encodeError = cause
			} else {
				manager.sendError = cause
			}
			source := pid.PID{Node: "local", Host: "process", UniqID: "sender"}
			target := pid.PID{Node: "remote-node", Host: "process", UniqID: "receiver"}
			pkg := relay.NewPackage(source, target, "data", payload.New("unchanged"))
			lease := &deliveryReleaseProbe{}
			pkg.Messages[0].SetRetentionLease(lease)
			err := service.Send(pkg)
			require.Error(t, err)
			require.Zero(t, lease.releases, "refused send must not recycle the caller-owned package")
			require.True(t, source.Equal(pkg.Source))
			require.True(t, target.Equal(pkg.Target))
			require.Equal(t, "unchanged", pkg.Messages[0].Payloads[0].Data())
			codec.encodeError, manager.sendError = nil, nil
			require.NoError(t, service.Send(pkg), "the same retained package must be retryable")
			require.Equal(t, 1, lease.releases, "successful admission releases exactly once")
		})
	}
}
