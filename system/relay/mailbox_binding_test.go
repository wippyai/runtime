// SPDX-License-Identifier: MPL-2.0
package relay

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestBoundMailboxNeverFollowsReattachment(t *testing.T) {
	// Control worker execution to prove that an already queued package cannot be
	// redirected when the destination changes before delivery.
	m := &Mailbox{ctx: context.Background(), config: mailboxConfig{workerCount: 1, logger: zap.NewNop()}, jobQueues: []chan mailboxJob{make(chan mailboxJob, 1)}}
	target := pid.PID{Node: "local", Host: "mail", UniqID: "same"}
	old, next := make(chan *api.Package, 1), make(chan *api.Package, 1)
	detach, err := m.Attach(target, old)
	require.NoError(t, err)
	bound, err := m.BindLocal(target)
	require.NoError(t, err)
	accepted := api.NewPackage(pid.PID{}, target, "data")
	require.NoError(t, bound.SendContext(context.Background(), accepted))
	detach()
	detachNext, err := m.Attach(target, next)
	require.NoError(t, err)
	defer detachNext()
	m.deliver(<-m.jobQueues[0])
	require.Empty(t, next, "queued old job must not reach new attachment")
	require.Empty(t, accepted.Messages, "discarded old job must release its package")
	refused := api.NewPackage(pid.PID{}, target, "data")
	require.ErrorIs(t, bound.SendContext(context.Background(), refused), api.ErrBindingRetired)
	require.Len(t, refused.Messages, 1)
	api.ReleasePackage(refused)
	fresh, err := m.BindLocal(target)
	require.NoError(t, err)
	pkg := api.NewPackage(pid.PID{}, target, "data")
	require.NoError(t, fresh.SendContext(context.Background(), pkg))
	m.deliver(<-m.jobQueues[0])
	require.Same(t, pkg, <-next)
	api.ReleasePackage(pkg)
}

func TestBoundMailboxTargetAndCancellation(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewMailbox(ctx, WithBufferSize(1))
	target := pid.PID{UniqID: "bound"}
	detach, err := m.Attach(target, make(chan *api.Package))
	require.NoError(t, err)
	defer detach()
	bound, err := m.BindLocal(target)
	require.NoError(t, err)
	wrong := api.NewPackage(pid.PID{}, pid.PID{UniqID: "other"}, "data")
	require.ErrorIs(t, bound.SendContext(ctx, wrong), api.ErrBindingTarget)
	api.ReleasePackage(wrong)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	pkg := api.NewPackage(pid.PID{}, target, "data")
	require.ErrorIs(t, bound.SendContext(canceled, pkg), context.Canceled)
	require.Len(t, pkg.Messages, 1)
	api.ReleasePackage(pkg)
}

func TestBoundMailboxDetachReleasesBlockedAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &Mailbox{ctx: context.Background(), config: mailboxConfig{workerCount: 1, logger: zap.NewNop()}, jobQueues: []chan mailboxJob{make(chan mailboxJob, 1)}}
		m.jobQueues[0] <- mailboxJob{} // no worker: admission must wait for capacity
		target := pid.PID{UniqID: "blocked"}
		detach, err := m.Attach(target, make(chan *api.Package))
		require.NoError(t, err)
		bound, err := m.BindLocal(target)
		require.NoError(t, err)
		pkg := api.NewPackage(pid.PID{}, target, "data")
		done := make(chan error, 1)
		go func() { done <- bound.SendContext(context.Background(), pkg) }()
		synctest.Wait() // sender has reached its blocking queue select
		select {
		case <-done:
			t.Fatal("full queue did not block sender")
		default:
		}
		detach()
		synctest.Wait()
		select {
		case err := <-done:
			require.ErrorIs(t, err, api.ErrBindingRetired)
		default:
			t.Fatal("retirement left bound admission blocked")
		}
		require.Len(t, pkg.Messages, 1)
		api.ReleasePackage(pkg)
		m.admissions.Wait() // canceled admission must not strand shutdown ownership
	})
}
