package pubsub

import (
	"context"
	"fmt"
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
				BufferSize:  100,
				WorkerCount: 4,
				Logger:      logger,
			},
		},
		{
			name: "custom configuration",
			config: HostConfig{
				BufferSize:  1000,
				WorkerCount: 8,
				Logger:      logger,
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
	ch1 := make(chan *api.Package, 10)
	cancel1, err1 := host.Attach(pid, ch1)
	assert.NoError(t, err1)
	assert.NotNil(t, cancel1)

	// Try duplicate attachment
	ch2 := make(chan *api.Package, 10)
	_, err2 := host.Attach(pid, ch2)
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
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	receiverCh := make(chan *api.Package, 1)
	_, err := host.Attach(pid, receiverCh)
	assert.NoError(t, err)

	pkg := &api.Package{
		PID: pid,
		Messages: []*api.Message{
			{Topic: "test", Payloads: nil},
		},
	}

	err = host.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestHost_SendCancelledContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  1,
		WorkerCount: 0, // no workers so jobCh is never drained
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	// Pre-fill the job channel
	pkg := &api.Package{
		PID: pid,
		Messages: []*api.Message{
			{Topic: "dummy"},
		},
	}
	err := host.Send(pkg)
	assert.NoError(t, err)
}

func TestHost_NoReceiver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	// send message without attaching a receiver
	pkg := &api.Package{
		PID: pid,
		Messages: []*api.Message{
			{Topic: "test"},
		},
	}
	err := host.Send(pkg)
	assert.NoError(t, err) // send should succeed even without receiver
}

func TestHost_DetachDuringDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	// Create a blocked receiver
	receiverCh := make(chan *api.Package)
	_, err := host.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// send message
	pkg := &api.Package{
		PID: pid,
		Messages: []*api.Message{
			{Topic: "test"},
		},
	}
	err = host.Send(pkg)
	assert.NoError(t, err)

	// Detach receiver during delivery attempt
	host.Detach(pid)

	// Allow some time for message processing
	time.Sleep(time.Millisecond * 100)
	// Type should be dropped without error
}

func TestHost_MultipleWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  100,
		WorkerCount: 4, // Multiple workers
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	receiverCh := make(chan *api.Package, 100)
	_, err := host.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// send multiple messages concurrently
	const messageCount = 50
	errorCh := make(chan error, messageCount)
	receivedCount := 0

	// send messages
	for i := 0; i < messageCount; i++ {
		go func(i int) {
			pkg := &api.Package{
				PID: pid,
				Messages: []*api.Message{
					{Topic: fmt.Sprintf("test-%d", i)},
				},
			}
			errorCh <- host.Send(pkg)
		}(i)
	}

	// Collect errors
	for i := 0; i < messageCount; i++ {
		err := <-errorCh
		assert.NoError(t, err)
	}

	// Collect received messages
	timeout := time.After(time.Second)
	for receivedCount < messageCount {
		select {
		case <-receiverCh:
			receivedCount++
		case <-timeout:
			t.Fatalf("timeout waiting for messages, received %d/%d", receivedCount, messageCount)
			return
		}
	}

	assert.Equal(t, messageCount, receivedCount)
}

func TestHost_HostShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ID{NS: "ns1", Name: "proc1"},
		UniqID: "uniq1",
	}

	receiverCh := make(chan *api.Package, 1)
	_, err := host.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// First ensure sending works before shutdown
	pkg := &api.Package{
		PID: pid,
		Messages: []*api.Message{
			{Topic: "test"},
		},
	}
	err = host.Send(pkg)
	assert.NoError(t, err)

	// Now cancel the host context
	cancel()
	time.Sleep(time.Millisecond * 10) // Give workers time to shut down

	// Try to send a message after shutdown
	err = host.Send(pkg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}
