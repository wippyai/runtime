// SPDX-License-Identifier: MPL-2.0

package relay_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/relay"
)

type contextRoute struct {
	entered chan context.Context
}

func (*contextRoute) Send(*api.Package) error { panic("legacy send must not be called") }
func (r *contextRoute) SendContext(ctx context.Context, _ *api.Package) error {
	r.entered <- ctx
	<-ctx.Done()
	return ctx.Err()
}

type contextRouteNode struct {
	*mockNode
	*contextRoute
}

func (n *contextRouteNode) Send(pkg *api.Package) error { return n.contextRoute.Send(pkg) }

func TestRouterContextCancellationAcrossRoutes(t *testing.T) {
	for _, target := range []string{"", "local", "peer", "remote"} {
		t.Run(target, func(t *testing.T) {
			localReceiver := &contextRoute{entered: make(chan context.Context, 1)}
			peerReceiver := &contextRoute{entered: make(chan context.Context, 1)}
			remoteReceiver := &contextRoute{entered: make(chan context.Context, 1)}
			local := &contextRouteNode{mockNode: &mockNode{id: "local"}, contextRoute: localReceiver}
			r := relay.NewRouter(local, remoteReceiver)
			if err := r.RegisterPeer("peer", peerReceiver); err != nil {
				t.Fatal(err)
			}
			pkg := &api.Package{Target: pid.PID{Node: target, Host: "worker"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.SendContext(ctx, pkg) }()
			var received context.Context
			var selected string
			select {
			case received = <-localReceiver.entered:
				selected = "local"
			case received = <-peerReceiver.entered:
				selected = "peer"
			case received = <-remoteReceiver.entered:
				selected = "remote"
			}
			expected := target
			if expected == "" {
				expected = "local"
			}
			if selected != expected {
				t.Fatalf("route = %s, want %s", selected, expected)
			}
			if received != ctx {
				t.Fatal("router did not propagate caller context")
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("delivery cancellation: %v", err)
			}
			if pkg.Target.Host != "worker" {
				t.Fatal("failed delivery consumed caller package")
			}
		})
	}
}

func TestRouterContextRejectsLegacyDestinations(t *testing.T) {
	for _, target := range []string{"local", "peer", "remote"} {
		t.Run(target, func(t *testing.T) {
			local := &mockNode{id: "local"}
			legacy := &mockReceiver{}
			r := relay.NewRouter(local, legacy)
			if err := r.RegisterPeer("peer", legacy); err != nil {
				t.Fatal(err)
			}
			pkg := &api.Package{Target: pid.PID{Node: target, Host: "worker"}}
			if err := r.SendContext(context.Background(), pkg); !errors.Is(err, relay.ErrContextUnsupported) {
				t.Fatalf("legacy delivery did not fail explicitly: %v", err)
			}
			if local.sendCalled != 0 || legacy.sendCalled != 0 {
				t.Fatal("cancellable path invoked legacy receiver")
			}
			// The historical API still supports that same receiver.
			if err := r.Send(pkg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRouterCanceledContextDoesNotDeliver(t *testing.T) {
	receiver := &contextRoute{entered: make(chan context.Context, 1)}
	r := relay.NewRouter(&mockNode{id: "local"}, receiver)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pkg := &api.Package{Target: pid.PID{Node: "remote", Host: "worker"}}
	if err := r.SendContext(ctx, pkg); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delivery: %v", err)
	}
	select {
	case <-receiver.entered:
		t.Fatal("already canceled call entered receiver")
	default:
	}
}
