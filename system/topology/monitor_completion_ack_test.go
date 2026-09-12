// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestMonitorCompletionAckCodecAndProvenance(t *testing.T) {
	want := monitorCompletionAck{caller: pid.PID{Node: "client", Host: "registry"}, target: pid.PID{Node: "server", Host: "process", UniqID: "actor"}, reference: "exact-reference"}
	for _, mode := range []string{"valid", "peer", "missing-peer", "source", "endpoint", "foreign-target", "missing-target-instance", "version", "empty-ref", "long-ref", "extra-field", "kind", "topic"} {
		t.Run(mode, func(t *testing.T) {
			original := want.packageForPeer()
			defer relay.ReleasePackage(original)
			wire := monitorCodecCopy(t, original, want.caller.Node)
			defer relay.ReleasePackage(wire)
			fields := wire.Messages[0].Payloads[0].Data().(map[string]any)
			switch mode {
			case "peer":
				wire.ReceivedFrom = "attacker"
			case "missing-peer":
				wire.ReceivedFrom = ""
			case "source":
				wire.Source.UniqID = "replacement"
			case "endpoint":
				wire.Target.Host = "process"
			case "foreign-target":
				target := want.target
				target.Node = "third"
				fields["target"] = target
			case "missing-target-instance":
				target := want.target
				target.UniqID = ""
				fields["target"] = target
			case "version":
				fields["v"] = uint64(2)
			case "empty-ref":
				fields["ref"] = ""
			case "long-ref":
				fields["ref"] = strings.Repeat("x", 65)
			case "extra-field":
				fields["success"] = true
			case "kind":
				fields["kind"] = "pid.monitor.result"
			case "topic":
				wire.Messages[0].Topic = "application"
			}
			got, err := decodeMonitorCompletionAck(wire, want.target.Node)
			if mode != "valid" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, got.caller.Equal(want.caller))
			require.True(t, got.target.Equal(want.target))
			require.Equal(t, want.reference, got.reference)
		})
	}
}

func TestMonitorCompletionAckSettlesOnlyCompletedExactObligation(t *testing.T) {
	caller := pid.PID{Node: "client", Host: "registry"}
	target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
	outbox, err := newMonitorOutbox(1, 4096)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "current", 1024))
	receive := func(reference string, peer pid.NodeID) (bool, error) {
		original := (monitorCompletionAck{caller: caller, target: target, reference: reference}).packageForPeer()
		defer relay.ReleasePackage(original)
		wire := monitorCodecCopy(t, original, peer)
		defer relay.ReleasePackage(wire)
		ack, err := decodeMonitorCompletionAck(wire, target.Node)
		if err != nil {
			return false, err
		}
		return outbox.ack(ack.target, ack.caller, ack.reference), nil
	}
	settled, err := receive("current", caller.Node)
	require.NoError(t, err)
	require.False(t, settled, "premature ACK cannot erase reserved completion capacity")
	_, err = outbox.fill(target, caller, "current", []byte("retained terminal notice"))
	require.NoError(t, err)
	settled, err = receive("old", caller.Node)
	require.NoError(t, err)
	require.False(t, settled)
	settled, err = receive("current", "attacker")
	require.Error(t, err)
	require.False(t, settled)
	_, retained := outbox.notice(target, caller, "current")
	require.True(t, retained)
	settled, err = receive("current", caller.Node)
	require.NoError(t, err)
	require.True(t, settled)
	settled, err = receive("current", caller.Node)
	require.NoError(t, err)
	require.False(t, settled, "duplicate ACK must not settle another obligation")
}
