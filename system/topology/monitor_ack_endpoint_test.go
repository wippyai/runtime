// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type monitorAckReleaseFunc func()

func (f monitorAckReleaseFunc) Release() { f() }

func TestMonitorAckEndpointSettlementAndOwnership(t *testing.T) {
	for _, mode := range []string{"accepted", "stale", "premature", "wrong-peer", "stopped", "canceled", "not-installed", "delivery-slots-full"} {
		t.Run(mode, func(t *testing.T) {
			caller := pid.PID{Node: "client", Host: "registry"}
			target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
			outbox, err := newMonitorOutbox(1, 4096)
			require.NoError(t, err)
			require.NoError(t, outbox.reserve(target, caller, "exact", 128))
			if mode != "premature" {
				_, err = outbox.fill(target, caller, "exact", []byte("terminal"))
				require.NoError(t, err)
			}
			exchange, err := newMonitorExchange(context.Background(), target.Node, monitorContextSender(func(context.Context, *relay.Package) error {
				t.Error("ACK settlement must not send network traffic")
				return nil
			}), 1, time.Second)
			require.NoError(t, err)
			defer exchange.stop()
			exchange.terminalOutbox = outbox
			ref := "exact"
			if mode == "stale" {
				ref = "old"
			}
			original := (monitorCompletionAck{caller: caller, target: target, reference: ref}).packageForPeer()
			defer relay.ReleasePackage(original)
			wire := monitorCodecCopy(t, original, caller.Node)
			releases := 0
			wire.Messages[0].SetRetentionLease(monitorAckReleaseFunc(func() { releases++ }))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "wrong-peer":
				wire.ReceivedFrom = "attacker"
			case "stopped":
				exchange.stop()
			case "canceled":
				cancel()
			case "not-installed":
				exchange.terminalOutbox = nil
			case "delivery-slots-full":
				exchange.deliveries = exchange.maximum
			}
			err = exchange.SendContext(ctx, wire)
			refused := mode == "wrong-peer" || mode == "stopped" || mode == "canceled" || mode == "not-installed"
			if refused {
				require.Error(t, err)
				require.Zero(t, releases, "refused ACK remains caller-owned")
				relay.ReleasePackage(wire)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, releases)
			_, retained := outbox.notice(target, caller, "exact")
			if mode == "premature" {
				require.True(t, outbox.release(target, caller, "exact"), "premature ACK must preserve admission capacity")
			} else {
				settled := mode == "accepted" || mode == "delivery-slots-full"
				require.Equal(t, !settled, retained)
			}
		})
	}
}

func TestMonitorAckEndpointReleasesPackageOutsideLifecycleLock(t *testing.T) {
	caller := pid.PID{Node: "client", Host: "registry"}
	target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
	exchange, err := newMonitorExchange(context.Background(), target.Node, monitorContextSender(func(context.Context, *relay.Package) error { return nil }), 1, time.Second)
	require.NoError(t, err)
	defer exchange.stop()
	exchange.terminalOutbox, err = newMonitorOutbox(1, 4096)
	require.NoError(t, err)
	original := (monitorCompletionAck{caller: caller, target: target, reference: "obsolete"}).packageForPeer()
	defer relay.ReleasePackage(original)
	wire := monitorCodecCopy(t, original, caller.Node)
	callbackStopped := false
	wire.Messages[0].SetRetentionLease(monitorAckReleaseFunc(func() {
		if exchange.mu.TryLock() {
			exchange.mu.Unlock()
			exchange.stop()
			callbackStopped = true
		}
	}))
	require.NoError(t, exchange.Send(wire))
	require.True(t, callbackStopped, "release callback must be able to reenter endpoint shutdown")
}
