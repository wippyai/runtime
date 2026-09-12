// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
)

func completionPackage(t *testing.T, f *monitorSenderFixture, reference string) *relay.Package {
	t.Helper()
	original := relay.NewPackage(f.target, pid.PID{Node: f.caller.Node, Host: monitorControlHostID}, topapi.TopicEvents, payload.New(map[string]any{
		"v": remoteMonitorVersion, "kind": topapi.Exit, "ref": reference, "caller": f.caller, "from": f.target, "result": nil,
	}))
	defer relay.ReleasePackage(original)
	return monitorCodecCopy(t, original, f.target.Node)
}

func TestMonitorCompletionBeforeAcknowledgmentCannotResurrectWatch(t *testing.T) {
	f := newMonitorSenderFixture(t)
	f.afterAdmission = func(*relay.Package) error { f.remote.Complete(f.target, &runtime.Result{}); return nil }
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.Len(t, f.deliveries, 1)
	require.Equal(t, topapi.Exit, f.deliveries[0]["kind"])
	require.NotContains(t, f.deliveries[0], "ref", "wire controls must stay out of consumer vocabulary")
	sh := f.local.getShard(f.caller.String())
	sh.mu.RLock()
	require.Empty(t, sh.processes[f.caller.String()].watching)
	require.True(t, sh.processes[f.caller.String()].remoteWatching[f.target.String()].terminal)
	sh.mu.RUnlock()
	require.ErrorIs(t, f.local.Monitor(f.caller, f.target), topapi.ErrPIDNotRegistered)
	require.NoError(t, f.local.Demonitor(f.caller, f.target))
}

func TestMonitorCompletionProvenanceAndExactReference(t *testing.T) {
	f := newMonitorSenderFixture(t)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	first := f.controls[0].reference
	for _, mode := range []string{"peer", "source", "caller", "reference"} {
		p := completionPackage(t, f, first)
		fields := p.Messages[0].Payloads[0].Data().(map[string]any)
		switch mode {
		case "peer":
			p.ReceivedFrom = "attacker"
		case "source":
			p.Source.UniqID = "another"
		case "caller":
			fields["caller"] = pid.PID{Node: "foreign", Host: "registry"}
		case "reference":
			fields["ref"] = "wrong"
		}
		err := f.local.monitorEndpoint.Send(p)
		if mode == "reference" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
			relay.ReleasePackage(p)
		}
		require.Empty(t, f.deliveries)
	}
	require.NoError(t, f.local.Demonitor(f.caller, f.target))
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.NoError(t, f.local.monitorEndpoint.Send(completionPackage(t, f, first)))
	require.Empty(t, f.deliveries, "old completion must not affect successor")
	current := f.controls[len(f.controls)-1].reference
	require.NoError(t, f.local.monitorEndpoint.Send(completionPackage(t, f, current)))
	require.NoError(t, f.local.monitorEndpoint.Send(completionPackage(t, f, current)))
	require.Len(t, f.deliveries, 1, "duplicate completion must not redeliver")
}

func TestMonitorCompletionLocalRefusalRemainsRetryable(t *testing.T) {
	f := newMonitorSenderFixture(t)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	p := completionPackage(t, f, f.controls[0].reference)
	f.deliveryErr = errors.New("local receiver at capacity")
	require.ErrorIs(t, f.local.monitorEndpoint.SendContext(context.Background(), p), f.deliveryErr)
	require.True(t, p.Source.Equal(f.target), "refusal must preserve sender package ownership")
	f.deliveryErr = nil
	require.NoError(t, f.local.monitorEndpoint.Send(p))
	require.Len(t, f.deliveries, 1)
}
