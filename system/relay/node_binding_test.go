// SPDX-License-Identifier: MPL-2.0
package relay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

type bindingHost struct {
	bind func(pid.PID) (api.ContextSender, error)
}

func (h *bindingHost) BindLocal(p pid.PID) (api.ContextSender, error) { return h.bind(p) }
func (*bindingHost) Send(*api.Package) error                          { panic("bound delivery must not use ordinary routing") }

type boundSendFunc func(context.Context, *api.Package) error

func (f boundSendFunc) SendContext(ctx context.Context, p *api.Package) error { return f(ctx, p) }

func TestBoundHostRetirementCancelsAndFencesAddressReuse(t *testing.T) {
	node := NewNode("local")
	router := NewRouter(node, &bindingReceiver{})
	target := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	entered, canceled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	host := &bindingHost{bind: func(pid.PID) (api.ContextSender, error) {
		return boundSendFunc(func(ctx context.Context, _ *api.Package) error {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-finish
			return ctx.Err()
		}), nil
	}}
	release, err := node.RegisterOwnedHost("app", host)
	require.NoError(t, err)
	bound, err := router.BindLocal(target)
	require.NoError(t, err)
	pkg := api.NewPackage(pid.PID{}, target, "data")
	done := make(chan error, 1)
	go func() { done <- bound.SendContext(context.Background(), pkg) }()
	<-entered
	release()
	<-canceled
	_, visible := node.GetHost("app")
	require.False(t, visible, "retired registration must stop new route lookups")
	replacementRelease, replacementErr := node.RegisterOwnedHost("app", host)
	// Always unblock the active call before assertions/cleanup.
	close(finish)
	deliveryErr := <-done
	if replacementRelease != nil {
		replacementRelease()
	}
	require.Error(t, replacementErr, "host address must remain reserved until old bound admission returns")
	require.ErrorIs(t, deliveryErr, context.Canceled)
	require.Len(t, pkg.Messages, 1)
	api.ReleasePackage(pkg)
	replacementRelease, err = node.RegisterOwnedHost("app", host)
	require.NoError(t, err)
	defer replacementRelease()
	old := api.NewPackage(pid.PID{}, target, "data")
	require.ErrorIs(t, bound.SendContext(context.Background(), old), api.ErrBindingRetired)
	api.ReleasePackage(old)
}

func TestRouterBindingCapturesMailboxAcrossHostReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := NewNode("local")
	router := NewRouter(node, &bindingReceiver{})
	target := pid.PID{Node: "local", Host: "mail", UniqID: "same"}
	oldMailbox, freshMailbox := NewMailbox(ctx), NewMailbox(ctx)
	oldChannel, freshChannel := make(chan *api.Package, 1), make(chan *api.Package, 1)
	detach, err := oldMailbox.Attach(target, oldChannel)
	require.NoError(t, err)
	defer detach()
	release, err := node.RegisterOwnedHost(target.Host, oldMailbox)
	require.NoError(t, err)
	bound, err := router.BindLocal(target)
	require.NoError(t, err)
	release()
	freshDetach, err := freshMailbox.Attach(target, freshChannel)
	require.NoError(t, err)
	defer freshDetach()
	releaseFresh, err := node.RegisterOwnedHost(target.Host, freshMailbox)
	require.NoError(t, err)
	defer releaseFresh()
	pkg := api.NewPackage(pid.PID{}, target, "data")
	require.ErrorIs(t, bound.SendContext(ctx, pkg), api.ErrBindingRetired)
	require.Empty(t, freshChannel)
	api.ReleasePackage(pkg)
	// Neither a configured physical fallback nor a local virtual peer can be
	// mistaken for a local destination binding.
	_, err = router.BindLocal(pid.PID{Node: "remote", Host: "mail", UniqID: "same"})
	require.Error(t, err)
	require.NoError(t, node.RegisterHost("unsupported", &dummyHost{}))
	_, err = router.BindLocal(pid.PID{Node: "local", Host: "unsupported"})
	require.ErrorIs(t, err, api.ErrBindingUnsupported)
}
