// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

func TestQueuedNativeIngressSurvivesPackageReleaseAndConnectionLoss(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`test_yield(1); return 1`, "ingress.lua"), WithModuleBinder(bindTestYield))
	parentIdentity := relay.IngressIdentity{Node: "must-not-inherit", Authenticated: true}
	ctx, _ := ctxapi.OpenFrameContext(context.WithValue(context.Background(), topicIngressKey{}, parentIdentity))
	require.NoError(t, proc.Init(ctx, "", nil))
	defer proc.Close()
	closed := make(chan struct{})
	observed := relay.IngressIdentity{Node: "immediate-peer", Authenticated: true, IntegrityProtected: true, ConnectionClosed: closed}
	pkg := relay.NewPackage(pid.PID{Node: "claimed-origin"}, pid.PID{}, "admission", payload.NewString("request"))
	pkg.Ingress = observed
	pkg.Messages[0].MaxItems = 4 // wait for the exact subscription
	var out processapi.StepOutput
	require.NoError(t, proc.Step([]processapi.Event{{Type: processapi.EventMessage, Data: pkg}}, &out))
	require.Len(t, proc.messageQueue, 1)
	require.Equal(t, observed, proc.messageQueue[0].Ingress)
	// Package ownership transferred to Step; the retained value must be independent
	// of its pooled envelope and still track the original connection closing.
	close(closed)
	_, err := proc.Subscribe("admission", 2)
	require.NoError(t, err)
	var deliveries []relay.IngressIdentity
	proc.SetTopicHandler("admission", func(ctx context.Context, _ *lua.LState, source pid.PID, _ string, _ []payload.Payload) lua.LValue {
		deliveries = append(deliveries, TopicIngress(ctx))
		return lua.LTrue
	})
	proc.flushMessageQueue(proc.subs)
	require.Equal(t, []relay.IngressIdentity{observed}, deliveries)
	select {
	case <-deliveries[0].ConnectionClosed:
	default:
		t.Fatal("lost exact closed connection lifetime")
	}
	proc.enqueueMessage(queuedMessage{Topic: "admission", Payloads: []payload.Payload{payload.NewString("local")}})
	proc.flushMessageQueue(proc.subs)
	require.Len(t, deliveries, 2)
	require.Equal(t, relay.IngressIdentity{}, deliveries[1], "local delivery must clear inherited remote provenance")
	require.Equal(t, parentIdentity, TopicIngress(proc.ctx), "delivery must not mutate the process context")
}
