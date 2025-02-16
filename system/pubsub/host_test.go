package pubsub

import (
	"context"
	api "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestHost_NewHost(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tests := []struct {
		name   string
		config HostConfig
	}{
		{
			name: "default configuration",
			config: HostConfig{
				BufferSize:   100,
				WorkerCount:  4,
				Logger:       logger,
				RetryTimeout: time.Millisecond * 100,
			},
		},
		{
			name: "custom configuration",
			config: HostConfig{
				BufferSize:   1000,
				WorkerCount:  8,
				Logger:       logger,
				RetryTimeout: time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := NewHost(ctx, tt.config)
			assert.NotNil(t, host)
			assert.Equal(t, tt.config.BufferSize, cap(host.jobCh))
			assert.Equal(t, tt.config.Logger, host.logger)
		})
	}
}

func TestHost_Attach(t *testing.T) {
	ctx := context.Background()
	host := NewHost(ctx, HostConfig{
		BufferSize:  100,
		WorkerCount: 4,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	// First attachment
	ch1 := make(chan *api.Batch, 10)
	err1, cancel1 := host.Attach(pid, ch1)
	assert.NoError(t, err1)
	assert.NotNil(t, cancel1)

	// Try duplicate attachment
	ch2 := make(chan *api.Batch, 10)
	err2, _ := host.Attach(pid, ch2)
	assert.Error(t, err2)
	assert.Equal(t, api.ErrAlreadyAttached, err2)

	// Test cancellation
	cancel1()
	time.Sleep(time.Millisecond * 10) // Allow time for the delete operation
	_, exists := host.receivers.Load(pid)
	assert.False(t, exists)
}

func TestHost_Send(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:   2,
		WorkerCount:  1,
		RetryTimeout: time.Millisecond * 100,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	receiverCh := make(chan *api.Batch, 1)
	err, _ := host.Attach(pid, receiverCh)
	assert.NoError(t, err)

	err = host.Send(ctx, pid, &api.Batch{{Topic: "test", Payloads: nil}})
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, &api.Batch{{Topic: "test", Payloads: nil}}, received)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestHost_SendCancelledContext(t *testing.T) {
	// Use a host with no workers to simulate a full job channel.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:   1,
		WorkerCount:  0, // no workers so jobCh is never drained
		RetryTimeout: time.Millisecond * 50,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	// Pre-fill the job channel.
	err := host.Send(context.Background(), pid, &api.Batch{{Topic: "dummy"}})
	assert.NoError(t, err)

	// Attempt to send with an already cancelled context.
	err = host.Send(cancelledContext(), pid, &api.Batch{{Topic: "cancelled"}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

func TestHost_SendBufferFull(t *testing.T) {
	// Use a host with no workers to simulate a full job channel.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:   1,
		WorkerCount:  0, // no worker, so jobCh remains full after one send
		RetryTimeout: time.Millisecond * 50,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	// First send should succeed and fill the job channel.
	err := host.Send(ctx, pid, &api.Batch{{Topic: "test1"}})
	assert.NoError(t, err)

	// Second send should fail with a timeout.
	err = host.Send(ctx, pid, &api.Batch{{Topic: "test2"}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
