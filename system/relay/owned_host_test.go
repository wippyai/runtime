// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

func TestOwnedHostReleasePreservesReplacement(t *testing.T) {
	node := NewNode("local")
	host := &dummyHost{}
	release, err := node.RegisterOwnedHost("control", host)
	require.NoError(t, err)
	duplicate, err := node.RegisterOwnedHost("control", &dummyHost{})
	require.Error(t, err)
	require.Nil(t, duplicate, "failed registration grants no cleanup authority")
	current, found := node.GetHost("control")
	require.True(t, found)
	require.Same(t, host, current)

	node.UnregisterHost("control")
	// Reusing the exact receiver must still create a new registration identity.
	replacementRelease, err := node.RegisterOwnedHost("control", host)
	require.NoError(t, err)
	defer replacementRelease()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { release() })
	}
	wg.Wait()
	current, found = node.GetHost("control")
	require.True(t, found)
	require.Same(t, host, current)
	replacementRelease()
	replacementRelease()
	_, found = node.GetHost("control")
	require.False(t, found)
}

func TestOwnedHostReleasePreservesLegacyRegistration(t *testing.T) {
	node := NewNode("local")
	host := &dummyHost{}
	release, err := node.RegisterOwnedHost("control", host)
	require.NoError(t, err)
	release()
	require.NoError(t, node.RegisterHost("control", host))
	release()
	current, found := node.GetHost("control")
	require.True(t, found)
	require.Same(t, host, current)
}

func TestOwnedHostRetainsReceiverCapabilities(t *testing.T) {
	node := NewNode("local")
	host := &dummyHost{}
	release, err := node.RegisterOwnedHost("control", host)
	require.NoError(t, err)
	defer release()
	target := pid.PID{Node: "local", Host: "control", UniqID: "actor"}
	detach, err := node.Attach(target, make(chan *api.Package))
	require.NoError(t, err)
	detach()
	require.EqualValues(t, 1, host.attachCalled)
	require.NoError(t, node.Send(&api.Package{Target: target}))
	require.EqualValues(t, 1, host.sendCalled)
}

func TestOwnedHostReleaseDoesNotDrainDispatchedCall(t *testing.T) {
	node := NewNode("local")
	host := &blockingHost{entered: make(chan struct{}), release: make(chan struct{})}
	release, err := node.RegisterOwnedHost("control", host)
	require.NoError(t, err)
	done := make(chan error, 1)
	target := pid.PID{Node: "local", Host: "control"}
	go func() { done <- node.Send(&api.Package{Target: target}) }()
	<-host.entered
	release()
	require.Error(t, node.SendContext(context.Background(), &api.Package{Target: target}))
	select {
	case <-done:
		t.Fatal("registration release must not complete a call already in the receiver")
	default:
	}
	close(host.release)
	require.NoError(t, <-done)
}

type ownedContextHost struct {
	dummyHost
	calls int
}

func (h *ownedContextHost) SendContext(context.Context, *api.Package) error {
	h.calls++
	return nil
}

func TestOwnedHostPreservesContextDelivery(t *testing.T) {
	node := NewNode("local")
	host := &ownedContextHost{}
	release, err := node.RegisterOwnedHost("control", host)
	require.NoError(t, err)
	defer release()
	receiver, found := node.GetHost("control")
	require.True(t, found)
	_, capable := receiver.(api.ContextSender)
	require.True(t, capable)
	require.NoError(t, node.SendContext(context.Background(), &api.Package{Target: pid.PID{Node: "local", Host: "control"}}))
	require.Equal(t, 1, host.calls)
	require.Zero(t, host.sendCalled, "must not fall back to legacy Send")
}
