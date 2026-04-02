// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap/zaptest"
)

const (
	testContainer = "wippy-test-redis"
	testPort      = "16379"
)

var testAddr string

func TestMain(m *testing.M) {
	if os.Getenv("REDIS_ADDR") != "" {
		testAddr = os.Getenv("REDIS_ADDR")
		os.Exit(m.Run())
	}

	if !dockerAvailable() {
		fmt.Println("docker not available, skipping Redis integration tests")
		os.Exit(0)
	}

	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()

	cmd := exec.CommandContext(context.Background(), "docker", "run", "-d",
		"--name", testContainer,
		"-p", testPort+":6379",
		"redis:8-alpine")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start redis: %s\n%s\n", err, out)
		os.Exit(1)
	}

	testAddr = "localhost:" + testPort

	if !waitForPort(testAddr, 30*time.Second) {
		fmt.Println("redis did not become ready in time")
		_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
		os.Exit(1)
	}

	code := m.Run()

	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
	os.Exit(code)
}

func dockerAvailable() bool {
	if runtime.GOOS == "windows" {
		// Check if Docker is in Linux containers mode (required for our test images).
		cmd := exec.CommandContext(context.Background(), "docker", "info", "--format", "{{.OSType}}")
		out, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(out)) != "linux" {
			return false
		}
		return true
	}
	cmd := exec.CommandContext(context.Background(), "docker", "info")
	return cmd.Run() == nil
}

func waitForPort(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", addr)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func setupDriver(t *testing.T) *Driver {
	t.Helper()
	logger := zaptest.NewLogger(t)
	opts := &goredis.Options{Addr: testAddr}
	driver := NewDriver(registry.ParseID("test:redis"), opts, logger)

	ctx := context.Background()
	statusCh, err := driver.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusCh)

	t.Cleanup(func() {
		_ = driver.Stop(ctx)
	})

	return driver
}

func TestRedisDriver_DeclareQueue(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueID := registry.ParseID("test:redis-declare")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, "test-declare-"+time.Now().Format("150405"))

	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	err = driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)
}

func TestRedisDriver_PublishAndAttach(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	streamName := "test-pubsub-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:redis-pubsub")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, streamName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("hello redis"))
	msg.ID = "redis-msg-1"
	msg.Headers.Set("custom", "header-value")

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	select {
	case delivery := <-deliveries:
		assert.Equal(t, "redis-msg-1", delivery.Message.ID)
		assert.NotNil(t, delivery.Message.Body)

		val, ok := delivery.Message.Headers.Get("custom")
		assert.True(t, ok)
		assert.Equal(t, "header-value", val)

		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestRedisDriver_MultipleMessages(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	streamName := "test-multi-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:redis-multi")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, streamName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		msg := queueapi.AcquireMessage(payload.New("msg"))
		msg.ID = string(rune('a' + i))
		err = driver.Publish(ctx, queueID, msg)
		require.NoError(t, err)
	}

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	received := 0
	timeout := time.After(10 * time.Second)
	for received < 3 {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			err = delivery.Ack(ctx)
			assert.NoError(t, err)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of 3 messages", received)
		}
	}
}

func TestRedisDriver_Nack(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	streamName := "test-nack-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:redis-nack")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, streamName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("nack-test"))
	msg.ID = "nack-msg-1"
	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	select {
	case delivery := <-deliveries:
		assert.Equal(t, "nack-msg-1", delivery.Message.ID)
		err = delivery.Nack(ctx)
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for first delivery")
	}
}

func TestRedisDriver_GetQueueInfo(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	streamName := "test-info-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:redis-info")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, streamName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg1 := queueapi.AcquireMessage(payload.New("info1"))
	msg1.ID = "info-1"
	msg2 := queueapi.AcquireMessage(payload.New("info2"))
	msg2.ID = "info-2"

	err = driver.Publish(ctx, queueID, msg1, msg2)
	require.NoError(t, err)

	info, err := driver.GetQueueInfo(ctx, queueID)
	require.NoError(t, err)

	count := info.GetInt(queueapi.StatsMessageCount, 0)
	assert.Equal(t, 2, count)
}

func TestRedisDriver_PublishNonExistent(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	msg := queueapi.AcquireMessage(payload.New("test"))
	err := driver.Publish(ctx, registry.ParseID("test:nonexistent"), msg)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}

func TestRedisDriver_AttachNonExistent(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	deliveries := make(chan *queueapi.Delivery, 10)
	_, err := driver.Attach(ctx, registry.ParseID("test:nonexistent"), deliveries)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}
