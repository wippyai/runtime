// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	socketapi "github.com/wippyai/runtime/api/socket"
)

func TestStartResolve_ACKBeforeCompletion(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	svc := &mockNetService{
		lookupFunc: func(_ context.Context, host string) ([]string, error) {
			assert.Equal(t, "example.com", host)
			close(entered)
			<-release
			return []string{"93.184.216.34"}, nil
		},
	}

	op := socketapi.NewPendingOperation()
	defer func() {
		unblock()
		_ = op.Close()
	}()

	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{
		Host:      "example.com",
		Operation: op,
	}

	require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
	ack := ackStart(t, recv)
	require.NoError(t, ack.Err)

	<-entered
	require.False(t, op.Ready(), "operation must not be ready before lookup completes")

	unblock()
	waitReady(t, op)

	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.NotNil(t, value)

	provider, ok := value.(socketapi.DNSAddressProvider)
	require.True(t, ok)
	assert.Equal(t, []string{"93.184.216.34"}, provider.DNSAddresses())
	require.NoError(t, value.Close())
}

func TestStartResolve_InheritedCancellation(t *testing.T) {
	entered := make(chan struct{})
	svc := &mockNetService{
		lookupFunc: func(ctx context.Context, _ string) ([]string, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	op := socketapi.NewPendingOperation()
	defer op.Close()

	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{
		Host:      "example.com",
		Operation: op,
	}

	require.NoError(t, d.handleResolve(ctx, cmd, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	<-entered
	cancel()

	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, takeErr, context.Canceled)
}

func TestStartResolve_ConfiguredDeadline(t *testing.T) {
	returned := make(chan struct{})

	svc := &mockNetService{
		lookupFunc: func(ctx context.Context, _ string) ([]string, error) {
			<-ctx.Done()
			close(returned)
			return nil, ctx.Err()
		},
	}

	op := socketapi.NewPendingOperation()
	defer op.Close()

	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{
		Host:      "example.com",
		Operation: op,
		Timeout:   15 * time.Millisecond,
	}

	require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	<-returned
	waitReady(t, op)

	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, takeErr, context.DeadlineExceeded)
}

func TestStartResolve_CloseBeforeTakeReleasesResult(t *testing.T) {
	svc := &mockNetService{
		lookupFunc: func(context.Context, string) ([]string, error) {
			return []string{"1.2.3.4"}, nil
		},
	}

	op := socketapi.NewPendingOperation()
	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{
		Host:      "example.com",
		Operation: op,
	}

	require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	waitReady(t, op)
	// Operation completed successfully, but caller closes before Take
	require.NoError(t, op.Close())

	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, err, socketapi.ErrOperationClosed)
}

func TestStartResolve_ResultSnapshotOwnership(t *testing.T) {
	sourceAddrs := []string{"10.0.0.1", "10.0.0.2"}
	svc := &mockNetService{
		lookupFunc: func(_ context.Context, _ string) ([]string, error) {
			return sourceAddrs, nil
		},
	}

	op := socketapi.NewPendingOperation()
	defer op.Close()

	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{
		Host:      "internal.corp",
		Operation: op,
	}

	require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.NotNil(t, value)

	// Consume via backend interface
	provider, ok := value.(socketapi.DNSAddressProvider)
	require.True(t, ok)

	// Mutate source slice
	sourceAddrs[0] = "mutated.ip"
	got1 := provider.DNSAddresses()
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, got1)

	// Mutate returned slice
	got1[0] = "mutated.local"
	got2 := provider.DNSAddresses()
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, got2)

	// Backend Close transfers ownership cleanup
	require.NoError(t, provider.Close())
	assert.Nil(t, provider.DNSAddresses())
}

func TestStartResolve_CountAndByteBounds(t *testing.T) {
	t.Run("exceeds max count", func(t *testing.T) {
		tooMany := make([]string, socketapi.MaxResolveAddresses+1)
		for i := range tooMany {
			tooMany[i] = "127.0.0.1"
		}
		svc := &mockNetService{
			lookupFunc: func(context.Context, string) ([]string, error) {
				return tooMany, nil
			},
		}

		op := socketapi.NewPendingOperation()
		defer op.Close()

		recv := newCaptureReceiver()
		d := NewDispatcher(svc)
		cmd := &socketapi.ResolveCmd{Host: "many.example", Operation: op}

		require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
		require.NoError(t, ackStart(t, recv).Err)

		waitReady(t, op)
		val, takeErr, ready := op.Take()
		require.True(t, ready)
		require.Nil(t, val)
		require.ErrorIs(t, takeErr, socketapi.ErrResolveLimit)
	})

	t.Run("exceeds max bytes", func(t *testing.T) {
		tooBig := strings.Repeat("x", socketapi.MaxResolveAddressBytes+1)
		svc := &mockNetService{
			lookupFunc: func(context.Context, string) ([]string, error) {
				return []string{tooBig}, nil
			},
		}

		op := socketapi.NewPendingOperation()
		defer op.Close()

		recv := newCaptureReceiver()
		d := NewDispatcher(svc)
		cmd := &socketapi.ResolveCmd{Host: "big.example", Operation: op}

		require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
		require.NoError(t, ackStart(t, recv).Err)

		waitReady(t, op)
		val, takeErr, ready := op.Take()
		require.True(t, ready)
		require.Nil(t, val)
		require.ErrorIs(t, takeErr, socketapi.ErrResolveLimit)
	})

	t.Run("within bounds succeeds", func(t *testing.T) {
		exactMax := make([]string, socketapi.MaxResolveAddresses)
		for i := range exactMax {
			exactMax[i] = "1.1.1.1"
		}
		svc := &mockNetService{
			lookupFunc: func(context.Context, string) ([]string, error) {
				return exactMax, nil
			},
		}

		op := socketapi.NewPendingOperation()
		defer op.Close()

		recv := newCaptureReceiver()
		d := NewDispatcher(svc)
		cmd := &socketapi.ResolveCmd{Host: "ok.example", Operation: op}

		require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
		require.NoError(t, ackStart(t, recv).Err)

		waitReady(t, op)
		val, takeErr, ready := op.Take()
		require.True(t, ready)
		require.NoError(t, takeErr)
		require.NotNil(t, val)
		require.NoError(t, val.Close())
	})
}

func TestStartResolve_LookupErrorDoesNotClone(t *testing.T) {
	lookupErr := errors.New("network unreachable")
	svc := &mockNetService{
		lookupFunc: func(context.Context, string) ([]string, error) {
			// Even if addresses slice was returned along with error, it must be discarded
			return []string{"should.not.be.cloned"}, lookupErr
		},
	}

	op := socketapi.NewPendingOperation()
	defer op.Close()

	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{Host: "fail.example", Operation: op}

	require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	waitReady(t, op)
	val, takeErr, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, val)
	require.ErrorIs(t, takeErr, lookupErr)
}

func TestStartResolve_WrongAndNilCommandHandling(t *testing.T) {
	svc := &mockNetService{
		lookupFunc: func(context.Context, string) ([]string, error) {
			t.Fatal("lookupFunc must not be called on invalid commands")
			return nil, nil
		},
	}
	d := NewDispatcher(svc)

	t.Run("nil command", func(t *testing.T) {
		recv := newCaptureReceiver()
		require.NoError(t, d.handleResolve(context.Background(), nil, 1, recv))
		require.ErrorIs(t, ackStart(t, recv).Err, socketapi.ErrNilOperation)
	})

	t.Run("wrong command type", func(t *testing.T) {
		recv := newCaptureReceiver()
		require.NoError(t, d.handleResolve(context.Background(), &socketapi.ConnectCmd{}, 2, recv))
		require.ErrorIs(t, ackStart(t, recv).Err, socketapi.ErrNilOperation)
	})

	t.Run("typed nil ResolveCmd", func(t *testing.T) {
		recv := newCaptureReceiver()
		var nilCmd *socketapi.ResolveCmd
		require.NoError(t, d.handleResolve(context.Background(), nilCmd, 3, recv))
		require.ErrorIs(t, ackStart(t, recv).Err, socketapi.ErrNilOperation)
	})

	t.Run("negative timeout", func(t *testing.T) {
		op := socketapi.NewPendingOperation()
		defer op.Close()
		recv := newCaptureReceiver()
		cmd := &socketapi.ResolveCmd{Host: "example.com", Operation: op, Timeout: -1}
		require.NoError(t, d.handleResolve(context.Background(), cmd, 4, recv))
		require.ErrorIs(t, ackStart(t, recv).Err, socketapi.ErrInvalidTimeout)
	})

	t.Run("already started operation", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }

		runningSvc := &mockNetService{
			lookupFunc: func(ctx context.Context, _ string) ([]string, error) {
				close(entered)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return []string{"1.2.3.4"}, nil
				}
			},
		}
		runningDispatcher := NewDispatcher(runningSvc)
		op := socketapi.NewPendingOperation()
		defer func() {
			unblock()
			_ = op.Close()
		}()

		recv1 := newCaptureReceiver()
		cmd1 := &socketapi.ResolveCmd{Host: "example.com", Operation: op}
		require.NoError(t, runningDispatcher.handleResolve(context.Background(), cmd1, 1, recv1))
		require.NoError(t, ackStart(t, recv1).Err)
		<-entered

		recv2 := newCaptureReceiver()
		cmd2 := &socketapi.ResolveCmd{Host: "example.com", Operation: op}
		require.NoError(t, runningDispatcher.handleResolve(context.Background(), cmd2, 2, recv2))
		require.ErrorIs(t, ackStart(t, recv2).Err, socketapi.ErrAlreadyStarted)
	})
}

func TestStartResolve_LegacyResolveCmdBehaviorPreserved(t *testing.T) {
	svc := &mockNetService{
		lookupFunc: func(_ context.Context, host string) ([]string, error) {
			if host == "error.invalid" {
				return nil, fmt.Errorf("name resolution error")
			}
			return []string{"192.168.1.100"}, nil
		},
	}
	d := NewDispatcher(svc)

	t.Run("legacy success with nil Operation", func(t *testing.T) {
		recv := newCaptureReceiver()
		cmd := &socketapi.ResolveCmd{
			Host:      "legacy.local",
			Operation: nil,
		}
		require.NoError(t, d.handleResolve(context.Background(), cmd, 10, recv))
		data, err := recv.wait()
		require.NoError(t, err)

		res, ok := data.(*socketapi.ResolveResult)
		require.True(t, ok, "legacy command must yield *socketapi.ResolveResult")
		require.NoError(t, res.Err)
		assert.Equal(t, []string{"192.168.1.100"}, res.Addresses)
	})

	t.Run("legacy error with nil Operation", func(t *testing.T) {
		recv := newCaptureReceiver()
		cmd := &socketapi.ResolveCmd{
			Host:      "error.invalid",
			Operation: nil,
		}
		require.NoError(t, d.handleResolve(context.Background(), cmd, 11, recv))
		data, err := recv.wait()
		require.NoError(t, err)

		res, ok := data.(*socketapi.ResolveResult)
		require.True(t, ok, "legacy command must yield *socketapi.ResolveResult")
		require.Error(t, res.Err)
		assert.Nil(t, res.Addresses)
	})
}

func TestStartResolve_CleanupUnblocksBeforeJoining(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	svc := &mockNetService{
		lookupFunc: func(ctx context.Context, _ string) ([]string, error) {
			close(entered)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return []string{"1.2.3.4"}, nil
			}
		},
	}

	op := socketapi.NewPendingOperation()
	recv := newCaptureReceiver()
	d := NewDispatcher(svc)
	cmd := &socketapi.ResolveCmd{Host: "example.com", Operation: op}

	require.NoError(t, d.handleResolve(context.Background(), cmd, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	<-entered
	// Ensure cleanup unblocks worker gate before closing and joining
	unblock()
	require.NoError(t, op.Close())
}

func TestStartResolveEmptySuccessIsNameUnresolvable(t *testing.T) {
	svc := &mockNetService{lookupFunc: func(context.Context, string) ([]string, error) { return nil, nil }}
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, NewDispatcher(svc).handleResolve(context.Background(), &socketapi.ResolveCmd{Host: "empty.example", Operation: op}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	result, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, result)
	var dnsErr *net.DNSError
	require.ErrorAs(t, err, &dnsErr)
	require.True(t, dnsErr.IsNotFound)
	require.Equal(t, "empty.example", dnsErr.Name)
}
