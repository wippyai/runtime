// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	"github.com/wippyai/runtime/service/queue/drivertest"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap/zaptest"
)

const (
	testContainer = "wippy-test-rabbitmq"
	testAMQPPort  = "25672"
)

var testAMQPURL string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping AMQP integration tests in short mode")
		os.Exit(0)
	}

	if os.Getenv("AMQP_URL") != "" {
		testAMQPURL = os.Getenv("AMQP_URL")
		os.Exit(m.Run())
	}

	if !drivertest.DockerAvailable() {
		fmt.Println("docker not available; running unit tests only")
		os.Exit(m.Run())
	}

	// Cleanup any leftover container.
	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()

	// Start RabbitMQ.
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

	// Wait for RabbitMQ AMQP protocol to be ready.
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
	if testAMQPURL == "" {
		t.Skip("AMQP URL not available (docker or AMQP_URL required)")
	}
	logger := zaptest.NewLogger(t)
	cfg := &amqpapi.Config{URL: testAMQPURL}
	tc := systempayload.NewTranscoder()
	sysjson.Register(tc)
	driver := NewDriver(registry.ParseID("test:amqp"), cfg, tc, logger)

	ctx := context.Background()
	statusCh, err := driver.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusCh)

	t.Cleanup(func() {
		_ = driver.Stop(ctx)
	})

	return driver
}

// TestAMQPDriver_Conformance runs the shared driver conformance suite.
func TestAMQPDriver_Conformance(t *testing.T) {
	driver := setupDriver(t)
	drivertest.New(t, driver,
		drivertest.WithTimeout(5*time.Second),
		drivertest.WithDeclareLeakProbe("amqp", map[string]any{
			"durable":      true,
			"auto_delete":  false,
			"max_length":   100,
			"message_ttl":  "15000",
			"queue_expiry": "600000",
		}),
	).Run()
}
