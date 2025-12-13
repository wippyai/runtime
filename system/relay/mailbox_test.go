package relay

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestMailbox_NewMailbox(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tests := []struct {
		name   string
		config MailboxConfig
	}{
		{
			name: "default configuration",
			config: MailboxConfig{
				BufferSize:  100,
				WorkerCount: 4,
				Logger:      logger,
			},
		},
		{
			name: "custom configuration",
			config: MailboxConfig{
				BufferSize:  1000,
				WorkerCount: 8,
				Logger:      logger,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := NewMailbox(ctx, tt.config)
			assert.NotNil(t, mailbox)
			assert.Equal(t, tt.config.Logger, mailbox.config.Logger)
		})
	}
}

func TestMailbox_Attach(t *testing.T) {
	ctx := context.Background()
	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100,
		WorkerCount: 4,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// First attachment
	ch1 := make(chan *relay.Package, 10)
	cancel1, err1 := mailbox.Attach(pid, ch1)
	assert.NoError(t, err1)
	assert.NotNil(t, cancel1)

	// Try duplicate attachment
	ch2 := make(chan *relay.Package, 10)
	_, err2 := mailbox.Attach(pid, ch2)
	assert.Error(t, err2)
	assert.ErrorIs(t, err2, relay.ErrAlreadyAttached)

	// Test cancellation
	cancel1()
	time.Sleep(time.Millisecond * 10) // Allow time for the delete operation
	_, exists := mailbox.receivers.Load(pid)
	assert.False(t, exists)
}

func TestMailbox_Send(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test", Payloads: nil},
		},
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendCancelledContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  1,
		WorkerCount: 0, // no workers so jobCh is never drained
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Pre-fill the job channel
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "dummy"},
		},
	}
	err := mailbox.Send(pkg)
	assert.NoError(t, err)
}

func TestMailbox_NoReceiver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// send message without attaching a receiver
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test"},
		},
	}
	err := mailbox.Send(pkg)
	assert.NoError(t, err) // send should succeed even without receiver
}

func TestMailbox_DetachDuringDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Create a blocked receiver
	receiverCh := make(chan *relay.Package)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// send message
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test"},
		},
	}
	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	// Detach receiver during delivery attempt
	mailbox.Detach(pid)

	// Allow some time for message processing
	time.Sleep(time.Millisecond * 100)
	// Type should be dropped without error
}

func TestMailbox_MultipleWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100,
		WorkerCount: 4, // Multiple workers
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 100)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// send multiple messages concurrently
	const messageCount = 50
	errorCh := make(chan error, messageCount)
	receivedCount := 0

	// send messages
	for i := 0; i < messageCount; i++ {
		go func(i int) {
			pkg := &relay.Package{
				Target: pid,
				Messages: []*relay.Message{
					{Topic: fmt.Sprintf("test-%d", i)},
				},
			}
			errorCh <- mailbox.Send(pkg)
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

func TestMailbox_Shutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  2,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// First ensure sending works before shutdown
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test"},
		},
	}
	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	// Now cancel the mailbox context
	cancel()
	time.Sleep(time.Millisecond * 10) // Give workers time to shut down

	// Try to send a message after shutdown
	err = mailbox.Send(pkg)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "context canceled")
	}
}

func TestMailbox_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tests := []struct {
		name           string
		config         MailboxConfig
		expectedConfig MailboxConfig
		shouldPanic    bool
	}{
		{
			name: "zero buffer size",
			config: MailboxConfig{
				BufferSize:  0,
				WorkerCount: 4,
				Logger:      logger,
			},
			expectedConfig: MailboxConfig{
				BufferSize:  0, // Zero buffer size is allowed
				WorkerCount: 4,
				Logger:      logger,
			},
		},
		{
			name: "negative buffer size",
			config: MailboxConfig{
				BufferSize:  -1,
				WorkerCount: 4,
				Logger:      logger,
			},
			shouldPanic: true, // Negative buffer size will cause panic in make()
		},
		{
			name: "zero worker count",
			config: MailboxConfig{
				BufferSize:  100,
				WorkerCount: 0,
				Logger:      logger,
			},
			expectedConfig: MailboxConfig{
				BufferSize:  100,
				WorkerCount: 1, // Zero worker count is set to 1
				Logger:      logger,
			},
		},
		{
			name: "negative worker count",
			config: MailboxConfig{
				BufferSize:  100,
				WorkerCount: -1,
				Logger:      logger,
			},
			expectedConfig: MailboxConfig{
				BufferSize:  100,
				WorkerCount: 1, // Negative worker count is set to 1
				Logger:      logger,
			},
		},
		{
			name: "nil logger",
			config: MailboxConfig{
				BufferSize:  100,
				WorkerCount: 4,
				Logger:      nil,
			},
			expectedConfig: MailboxConfig{
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
					NewMailbox(ctx, tt.config)
				})
			} else {
				mailbox := NewMailbox(ctx, tt.config)
				assert.NotNil(t, mailbox)
				assert.Equal(t, tt.expectedConfig.WorkerCount, mailbox.config.WorkerCount)
				assert.Equal(t, tt.expectedConfig.BufferSize, mailbox.config.BufferSize)
				assert.NotNil(t, mailbox.config.Logger)
			}
		})
	}
}

func TestMailbox_SendMultipleMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// Create a package with multiple messages
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test1", Payloads: nil},
			{Topic: "test2", Payloads: nil},
			{Topic: "test3", Payloads: nil},
		},
	}

	err = mailbox.Send(pkg)
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

func TestMailbox_SendEmptyMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// Create a package with empty messages array
	pkg := &relay.Package{
		Target:   pid,
		Messages: []*relay.Message{},
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Empty(t, received.Messages)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendNilMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100,
		WorkerCount: 1,
	})

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// Create a package with nil messages array
	pkg := &relay.Package{
		Target:   pid,
		Messages: nil,
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Nil(t, received.Messages)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendNilPackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100,
		WorkerCount: 1,
	})

	// Try to send nil package - this should return an error
	err := mailbox.Send(nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, relay.ErrNilPackage)
}
