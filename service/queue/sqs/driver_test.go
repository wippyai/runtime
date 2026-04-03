// SPDX-License-Identifier: MPL-2.0

package sqs

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

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	sqsapi "github.com/wippyai/runtime/api/service/queue/sqs"
	"go.uber.org/zap/zaptest"
)

const (
	testContainer = "wippy-test-elasticmq"
	testPort      = "19324"
)

var testEndpoint string

func TestMain(m *testing.M) {
	if os.Getenv("SQS_ENDPOINT") != "" {
		testEndpoint = os.Getenv("SQS_ENDPOINT")
		os.Exit(m.Run())
	}

	if !dockerAvailable() {
		fmt.Println("docker not available, skipping SQS integration tests")
		os.Exit(0)
	}

	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()

	cmd := exec.CommandContext(context.Background(), "docker", "run", "-d",
		"--name", testContainer,
		"-p", testPort+":9324",
		"softwaremill/elasticmq-native")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start elasticmq: %s\n%s\n", err, out)
		os.Exit(1)
	}

	testEndpoint = "http://localhost:" + testPort

	if !waitForPort("localhost:"+testPort, 30*time.Second) {
		fmt.Println("elasticmq did not become ready in time")
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
	ctx := context.Background()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("elasticmq"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("x", "x", "x")),
	)
	require.NoError(t, err)
	awsCfg.BaseEndpoint = aws.String(testEndpoint)

	sqsCfg := &sqsapi.Config{}
	sqsCfg.InitDefaults()

	driver := NewDriver(registry.ParseID("test:sqs"), sqsCfg, awsCfg, logger)

	statusCh, err := driver.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusCh)

	t.Cleanup(func() {
		_ = driver.Stop(ctx)
	})

	return driver
}

func TestSQSDriver_DeclareQueue(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueID := registry.ParseID("test:sqs-declare")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, "test-declare-"+time.Now().Format("150405"))

	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	err = driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)
}

func TestSQSDriver_PublishAndAttach(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-pubsub-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:sqs-pubsub")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("hello sqs"))
	msg.ID = "sqs-msg-1"
	msg.Headers.Set("custom", "header-value")

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	select {
	case delivery := <-deliveries:
		assert.NotEmpty(t, delivery.Message.ID)
		assert.NotNil(t, delivery.Message.Body)
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestSQSDriver_MultipleMessages(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-multi-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:sqs-multi")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
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
	timeout := time.After(30 * time.Second)
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

func TestSQSDriver_Nack(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	queueName := "test-nack-" + time.Now().Format("150405")
	queueID := registry.ParseID("test:sqs-nack")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, queueName)
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
		assert.NotEmpty(t, delivery.Message.ID)
		err = delivery.Nack(ctx)
		assert.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for first delivery")
	}

	select {
	case delivery := <-deliveries:
		assert.NotEmpty(t, delivery.Message.ID)
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for redelivery")
	}
}

func TestSQSDriver_PublishNonExistent(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	msg := queueapi.AcquireMessage(payload.New("test"))
	err := driver.Publish(ctx, registry.ParseID("test:nonexistent"), msg)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}

func TestSQSDriver_AttachNonExistent(t *testing.T) {
	driver := setupDriver(t)

	ctx := context.Background()
	deliveries := make(chan *queueapi.Delivery, 10)
	_, err := driver.Attach(ctx, registry.ParseID("test:nonexistent"), deliveries)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}
