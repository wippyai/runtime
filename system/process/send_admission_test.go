// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

func TestProcessSendReportsRemoteAdmissionFailure(t *testing.T) {
	cfg := internode.DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	manager := internode.NewConnectionManager(cfg, nil)
	transport := internode.NewService(zap.NewNop(), manager, internode.NewMessageCodec(payload.NewTranscoder()), nil, nil, nil)
	router := relay.NewRouter(relay.NewNode("local"), transport)
	d := NewDispatcher(nil, router, nil, nil)
	cmd := &api.SendCmd{From: pid.PID{Node: "local", Host: "process", UniqID: "sender"},
		To: pid.PID{Node: "remote", Host: "process", UniqID: "target"}, Topic: "request"}
	for _, managed := range []bool{false, true, false, true} {
		if managed {
			manager.AddManagedNode("remote")
		} else {
			manager.RemoveManagedNode("remote")
		}
		receiver := &mockResultReceiver{}
		require.NoError(t, d.handleSend(t.Context(), cmd, 1, receiver))
		require.NoError(t, receiver.err)
		result, ok := receiver.data.(api.SendResult)
		require.True(t, ok)
		if managed {
			require.NoError(t, result.Error)
		} else {
			require.ErrorIs(t, result.Error, internode.ErrNodeNotManaged)
			var typed apierror.Error
			require.ErrorAs(t, result.Error, &typed)
			require.Equal(t, apierror.Unavailable, typed.Kind())
			require.ErrorContains(t, result.Error, "remote")
		}
	}
	manager.RemoveManagedNode("remote")
}
