// SPDX-License-Identifier: MPL-2.0

package eventual_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func startedService(t *testing.T, node string) *eventual.Service {
	t.Helper()
	svc := eventual.NewService(eventual.Config{LocalNodeID: node})
	require.NoError(t, svc.Start(context.Background()))
	t.Cleanup(func() { _ = svc.Stop() })
	return svc
}

func TestAwait_ReturnsALiveNameImmediately(t *testing.T) {
	svc := startedService(t, "node-A")
	p := pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"}
	_, err := svc.Register("svc.live", p)
	require.NoError(t, err)

	got, err := svc.Await(context.Background(), "svc.live")
	require.NoError(t, err)
	require.Equal(t, p, got)
}

func TestAwait_WakesWhenTheNameIsRegisteredLocally(t *testing.T) {
	svc := startedService(t, "node-A")
	p := pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"}
	result := make(chan pid.PID, 1)
	go func() {
		got, err := svc.Await(context.Background(), "svc.later")
		if err == nil {
			result <- got
		}
	}()
	require.Eventually(t, func() bool { return svc.Waiting() == 1 }, time.Second, time.Millisecond)

	_, err := svc.Register("svc.later", p)
	require.NoError(t, err)
	select {
	case got := <-result:
		require.Equal(t, p, got)
	case <-time.After(time.Second):
		t.Fatal("Await did not wake on local registration")
	}
	require.Eventually(t, func() bool { return svc.Waiting() == 0 }, time.Second, time.Millisecond)
}

func TestAwait_WakesWhenTheNameArrivesFromAPeer(t *testing.T) {
	svc := startedService(t, "node-A")
	p := pid.PID{Node: "node-B", Host: "workers", UniqID: "sup"}
	result := make(chan pid.PID, 1)
	go func() {
		got, err := svc.Await(context.Background(), "bee.hive.supervisor/node-B")
		if err == nil {
			result <- got
		}
	}()
	require.Eventually(t, func() bool { return svc.Waiting() == 1 }, time.Second, time.Millisecond)

	svc.OnFrame(remoteDeltaFrame(t, "bee.hive.supervisor/node-B", p, "node-B", 1))
	select {
	case got := <-result:
		require.Equal(t, p, got)
	case <-time.After(time.Second):
		t.Fatal("Await did not wake when the name arrived from a peer")
	}
}

func TestAwait_EndsWithTheContextAndLeavesNoWaiter(t *testing.T) {
	svc := startedService(t, "node-A")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := svc.Await(ctx, "svc.never")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, svc.Waiting())
}

func TestAwait_EndsWhenTheServiceStops(t *testing.T) {
	svc := startedService(t, "node-A")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := svc.Await(ctx, "svc.never"); result <- err }()
	require.Eventually(t, func() bool { return svc.Waiting() == 1 }, time.Second, time.Millisecond)
	require.NoError(t, svc.Stop())
	select {
	case err := <-result:
		require.ErrorIs(t, err, eventual.ErrServiceStopped)
	case <-time.After(time.Second):
		t.Fatal("Await outlived the registry service")
	}
	require.Zero(t, svc.Waiting())
	_, err := svc.Await(ctx, "svc.after-stop")
	require.ErrorIs(t, err, eventual.ErrServiceStopped)
}

func TestAwait_CanceledContextDoesNotReturnALiveName(t *testing.T) {
	svc := startedService(t, "node-A")
	_, err := svc.Register("svc.live", pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.Await(ctx, "svc.live")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, svc.Waiting())
}

func TestAwait_ManyWaitersOnOneNameAllWake(t *testing.T) {
	svc := startedService(t, "node-A")
	p := pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"}
	const waiters = 32
	done := make(chan struct{}, waiters)
	for range waiters {
		go func() {
			if got, err := svc.Await(context.Background(), "svc.shared"); err == nil && got == p {
				done <- struct{}{}
			}
		}()
	}
	require.Eventually(t, func() bool { return svc.Waiting() == waiters }, time.Second, time.Millisecond)
	_, err := svc.Register("svc.shared", p)
	require.NoError(t, err)
	for range waiters {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("not every waiter woke")
		}
	}
	require.Eventually(t, func() bool { return svc.Waiting() == 0 }, time.Second, time.Millisecond)
}
