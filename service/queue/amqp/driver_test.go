// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap/zaptest"
)

const (
	testContainer = "wippy-test-rabbitmq"
	testAMQPPort  = "25672"
)

var testAMQPURL string

func TestMain(m *testing.M) {
	if os.Getenv("AMQP_URL") != "" {
		testAMQPURL = os.Getenv("AMQP_URL")
		os.Exit(m.Run())
	}

	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("docker not found, skipping AMQP integration tests")
		os.Exit(0)
	}

	// Cleanup any leftover container
	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()

	// Start RabbitMQ
	cmd := exec.CommandContext(context.Background(), "docker", "run", "-d",
		"--name", testContainer,
		"-p", testAMQPPort+":5672",
		"-e", "RABBITMQ_DEFAULT_USER=guest",
		"-e", "RABBITMQ_DEFAULT_PASS=guest",
		"rabbitmq:3-management")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start rabbitmq: %s\n%s\n", err, out)
		os.Exit(1)
	}

	testAMQPURL = fmt.Sprintf("amqp://guest:guest@localhost:%s/", testAMQPPort)

	// Wait for RabbitMQ AMQP protocol to be ready
	if !waitForAMQP(testAMQPURL, 60*time.Second) {
		fmt.Println("rabbitmq did not become ready in time")
		_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
		os.Exit(1)
	}

	code := m.Run()

	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
	os.Exit(code)
}

func waitForAMQP(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := amqp091.Dial(url)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func setupDriver(t *testing.T) *Driver {
	t.Helper()
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:amqp"), testAMQPURL, logger)

	ctx := context.Background()
	statusCh, err := driver.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusCh)

	t.Cleanup(func() {
		_ = driver.Stop(ctx)
	})

	return driver
}

func TestAMQPDriver_DeclareQueue(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueID := registry.ParseID("test:amqp-declare")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, "test-declare-"+time.Now().Format("150405"))

	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	// Declaring again should be idempotent
	err = driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)
}

func TestAMQPDriver_PublishAndAttach(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-pubsub-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:amqp-pubsub")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("hello amqp"))
	msg.ID = "amqp-msg-1"
	msg.Headers.Set("custom", "header-value")

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		assert.Equal(t, "amqp-msg-1", delivery.Message.ID)
		assert.NotNil(t, delivery.Message.Body)
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestAMQPDriver_MultipleMessages(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-multi-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:amqp-multi")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	for i := 0; i < 3; i++ {
		msg := queueapi.AcquireMessage(payload.New("msg"))
		msg.ID = string(rune('a' + i))
		err = driver.Publish(ctx, queueID, msg)
		require.NoError(t, err)
	}

	received := 0
	timeout := time.After(5 * time.Second)
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

func TestAMQPDriver_Nack(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-nack-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:amqp-nack")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("nack-test"))
	msg.ID = "nack-msg-1"
	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		assert.Equal(t, "nack-msg-1", delivery.Message.ID)
		err = delivery.Nack(ctx)
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first delivery")
	}

	select {
	case delivery := <-deliveries:
		assert.Equal(t, "nack-msg-1", delivery.Message.ID)
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for redelivery")
	}
}

func TestAMQPDriver_GetQueueInfo(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-info-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:amqp-info")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
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

func TestAMQPDriver_PublishNonExistent(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	msg := queueapi.AcquireMessage(payload.New("test"))
	err := driver.Publish(ctx, registry.ParseID("test:nonexistent"), msg)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}

func TestAMQPDriver_AttachNonExistent(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	deliveries := make(chan *queueapi.Delivery, 10)
	_, err := driver.Attach(ctx, registry.ParseID("test:nonexistent"), deliveries)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}
