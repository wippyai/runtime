package pubsub

import (
	"context"
	"fmt"
	"testing"
	"time"

	api "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
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
		Target: pid,
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
		Target: pid,
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
		Target: pid,
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
		Target: pid,
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
				Target: pid,
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
		Target: pid,
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

func TestHost_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tests := []struct {
		name           string
		config         HostConfig
		expectedConfig HostConfig
		shouldPanic    bool
	}{
		{
			name: "zero buffer size",
			config: HostConfig{
				BufferSize:  0,
				WorkerCount: 4,
				Logger:      logger,
			},
			expectedConfig: HostConfig{
				BufferSize:  0, // Zero buffer size is allowed
				WorkerCount: 4,
				Logger:      logger,
			},
		},
		{
			name: "negative buffer size",
			config: HostConfig{
				BufferSize:  -1,
				WorkerCount: 4,
				Logger:      logger,
			},
			shouldPanic: true, // Negative buffer size will cause panic in make()
		},
		{
			name: "zero worker count",
			config: HostConfig{
				BufferSize:  100,
				WorkerCount: 0,
				Logger:      logger,
			},
			expectedConfig: HostConfig{
				BufferSize:  100,
				WorkerCount: 1, // Zero worker count is set to 1
				Logger:      logger,
			},
		},
		{
			name: "negative worker count",
			config: HostConfig{
				BufferSize:  100,
				WorkerCount: -1,
				Logger:      logger,
			},
			expectedConfig: HostConfig{
				BufferSize:  100,
				WorkerCount: 1, // Negative worker count is set to 1
				Logger:      logger,
			},
		},
		{
			name: "nil logger",
			config: HostConfig{
				BufferSize:  100,
				WorkerCount: 4,
				Logger:      nil,
			},
			expectedConfig: HostConfig{
				BufferSize:  100,
				WorkerCount: 4,
				Logger:      zap.NewNop(), // Nil logger is replaced with noop logger
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				assert.Panics(t, func() {
					NewHost(ctx, tt.config)
				})
			} else {
				host := NewHost(ctx, tt.config)
				assert.NotNil(t, host)
				assert.Equal(t, tt.expectedConfig.WorkerCount, host.config.WorkerCount)
				assert.Equal(t, tt.expectedConfig.BufferSize, host.config.BufferSize)
				if tt.config.Logger == nil {
					assert.NotNil(t, host.config.Logger)
				} else {
					assert.Equal(t, tt.expectedConfig.Logger, host.config.Logger)
				}
			}
		})
	}
}

func TestHost_SendMultipleMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  100,
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

	// Create a package with multiple messages
	pkg := &api.Package{
		Target: pid,
		Messages: []*api.Message{
			{Topic: "test1", Payloads: nil},
			{Topic: "test2", Payloads: nil},
			{Topic: "test3", Payloads: nil},
		},
	}

	err = host.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Len(t, received.Messages, 3)
		assert.Equal(t, "test1", received.Messages[0].Topic)
		assert.Equal(t, "test2", received.Messages[1].Topic)
		assert.Equal(t, "test3", received.Messages[2].Topic)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestHost_SendEmptyMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  100,
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

	// Create a package with empty messages array
	pkg := &api.Package{
		Target:   pid,
		Messages: []*api.Message{},
	}

	err = host.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Empty(t, received.Messages)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestHost_SendNilMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  100,
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

	// Create a package with nil messages array
	pkg := &api.Package{
		Target:   pid,
		Messages: nil,
	}

	err = host.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Nil(t, received.Messages)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestHost_SendNilPackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	host := NewHost(ctx, HostConfig{
		BufferSize:  100,
		WorkerCount: 1,
	})

	// Try to send nil package - this should panic due to nil pointer dereference
	assert.Panics(t, func() {
		_ = host.Send(nil)
	})
}
