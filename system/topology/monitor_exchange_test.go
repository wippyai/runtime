// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
)

type monitorContextSender func(context.Context, *relay.Package) error

// This fixture is one immutable receiver lifetime; no routing lookup occurs.
func (s monitorContextSender) BindLocal(target pid.PID) (relay.ContextSender, error) {
	return monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		if !p.Target.Equal(target) {
			return relay.ErrBindingTarget
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return s(ctx, p)
	}), nil
}

func (s monitorContextSender) SendContext(ctx context.Context, p *relay.Package) error {
	return s(ctx, p)
}
func (s monitorContextSender) Send(p *relay.Package) error { return s(context.Background(), p) }

func monitorCodecCopy(t *testing.T, p *relay.Package, peer pid.NodeID) *relay.Package {
	t.Helper()
	codec := internode.NewMessageCodec(nil)
	wire, err := codec.Encode(p)
	require.NoError(t, err)
	decoded, err := codec.Decode(wire)
	require.NoError(t, err)
	decoded.ReceivedFrom = peer
	return decoded
}

func TestMonitorExchangeNativeAdmissionAndAuthenticatedReply(t *testing.T) {
	caller := pid.PID{Node: "client", Host: "sysreg"}
	target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
	remote := NewTopology(nodeExitBoundaryRouter(func(p *relay.Package) error { relay.ReleasePackage(p); return nil }), target.Node)
	require.NoError(t, remote.Register(target))
	var exchange *monitorExchange
	sender := monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		request := monitorCodecCopy(t, p, caller.Node)
		defer relay.ReleasePackage(request)
		handled, reply, err := remote.PrepareRemoteMonitorReply(request, 1)
		require.True(t, handled)
		require.NoError(t, err)
		require.NotNil(t, reply)
		defer relay.ReleasePackage(reply)
		// A valid correlation is insufficient without the exact authenticated peer
		// and process source. A refusal leaves ownership with the submitting sender.
		for _, mode := range []string{"peer", "source", "reference", "operation"} {
			forged := monitorCodecCopy(t, reply, target.Node)
			switch mode {
			case "peer":
				forged.ReceivedFrom = "attacker"
			case "source":
				forged.Source.UniqID = "different"
			case "reference":
				forged.Messages[0].Payloads[0].Data().(map[string]any)["ref"] = "unknown"
			case "operation":
				fields := forged.Messages[0].Payloads[0].Data().(map[string]any)
				if fields["operation"] == topapi.MonitorRequest {
					fields["operation"] = topapi.MonitorRelease
				} else {
					fields["operation"] = topapi.MonitorRequest
				}
			}
			err := exchange.SendContext(ctx, forged)
			if mode == "peer" || mode == "source" {
				require.Error(t, err)
				relay.ReleasePackage(forged)
			} else {
				require.NoError(t, err)
			}
			exchange.mu.Lock()
			for _, pending := range exchange.pending {
				require.Empty(t, pending.result, "invalid reply completed waiter")
			}
			exchange.mu.Unlock()
		}
		require.NoError(t, exchange.SendContext(ctx, monitorCodecCopy(t, reply, target.Node)))
		relay.ReleasePackage(p)
		return nil
	})
	var err error
	exchange, err = newMonitorExchange(context.Background(), caller.Node, sender, 1, time.Second)
	require.NoError(t, err)
	defer exchange.stop()
	control := remoteMonitorControl{kind: topapi.MonitorRequest, reference: "one", caller: caller, target: target}
	require.NoError(t, exchange.roundTrip(context.Background(), control))
	control.kind = topapi.MonitorRelease
	require.NoError(t, exchange.roundTrip(context.Background(), control))
	control.kind = topapi.MonitorRequest
	require.ErrorIs(t, exchange.roundTrip(context.Background(), control), errRemoteMonitorConflict)
	control.previous, control.reference = "one", "two"
	require.NoError(t, exchange.roundTrip(context.Background(), control))
}

func TestMonitorExchangeStopJoinsBlockedSendAndBoundsAdmission(t *testing.T) {
	entered := make(chan struct{})
	sender := monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err() // package remains owned by roundTrip on refusal
	})
	exchange, err := newMonitorExchange(context.Background(), "client", sender, 1, time.Minute)
	require.NoError(t, err)
	defer exchange.stop()
	control := remoteMonitorControl{kind: topapi.MonitorRequest, reference: "one", caller: pid.PID{Node: "client", Host: "registry"}, target: pid.PID{Node: "server", Host: "process", UniqID: "actor"}}
	done := make(chan error, 1)
	go func() { done <- exchange.roundTrip(context.Background(), control) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("send never entered")
	}
	control.reference = "two"
	require.ErrorIs(t, exchange.roundTrip(context.Background(), control), errRemoteMonitorCapacity)
	exchange.stop()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Empty(t, exchange.pending)
	require.ErrorIs(t, exchange.roundTrip(context.Background(), control), context.Canceled)
}

func TestMonitorExchangeLostReplyPreservesInstalledRelationship(t *testing.T) {
	caller := pid.PID{Node: "client", Host: "registry"}
	target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
	remote := NewTopology(nodeExitBoundaryRouter(func(p *relay.Package) error { relay.ReleasePackage(p); return nil }), target.Node)
	require.NoError(t, remote.Register(target))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := monitorContextSender(func(_ context.Context, p *relay.Package) error {
		request := monitorCodecCopy(t, p, caller.Node)
		defer relay.ReleasePackage(request)
		handled, reply, err := remote.PrepareRemoteMonitorReply(request, 1)
		require.True(t, handled)
		require.NoError(t, err)
		require.NotNil(t, reply)
		// Installation committed, but the reply is lost and the caller cancels.
		relay.ReleasePackage(reply)
		relay.ReleasePackage(p)
		cancel()
		return nil
	})
	exchange, err := newMonitorExchange(context.Background(), caller.Node, sender, 1, time.Second)
	require.NoError(t, err)
	defer exchange.stop()
	control := remoteMonitorControl{kind: topapi.MonitorRequest, reference: "one", caller: caller, target: target}
	require.ErrorIs(t, exchange.roundTrip(ctx, control), context.Canceled)
	require.Empty(t, exchange.pending)
	// Only the exact reference can retry idempotently; a fresh initial request
	// cannot replace an uncertain installation by claiming no predecessor.
	for _, attempt := range []struct {
		reference string
		want      error
	}{{"one", nil}, {"two", errRemoteMonitorConflict}} {
		p := monitorAdmissionPackage(t, caller, target, topapi.MonitorRequest, "", attempt.reference)
		_, err := remote.HandleRemoteMonitor(p, 1)
		relay.ReleasePackage(p)
		require.ErrorIs(t, err, attempt.want)
	}
}

func TestMonitorExchangeRejectsInvalidOutboundBeforeSend(t *testing.T) {
	exchange, err := newMonitorExchange(context.Background(), "client", monitorContextSender(func(context.Context, *relay.Package) error { t.Fatal("invalid request reached transport"); return nil }), 1, time.Second)
	require.NoError(t, err)
	defer exchange.stop()
	base := remoteMonitorControl{kind: topapi.MonitorRequest, reference: "one", caller: pid.PID{Node: "client", Host: "registry"}, target: pid.PID{Node: "server", Host: "process", UniqID: "actor"}}
	for _, mode := range []string{"target", "self-predecessor", "large-predecessor", "release-predecessor"} {
		c := base
		switch mode {
		case "target":
			c.target.UniqID = ""
		case "self-predecessor":
			c.previous = c.reference
		case "large-predecessor":
			c.previous = string(make([]byte, 65))
		case "release-predecessor":
			c.kind, c.previous = topapi.MonitorRelease, "old"
		}
		require.Error(t, exchange.roundTrip(context.Background(), c), mode)
	}
	require.Error(t, exchange.SendContext(nil, nil))
	require.Empty(t, exchange.pending)
}
