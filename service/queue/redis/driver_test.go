// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/queue/drivertest"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap/zaptest"
)

const (
	testContainer = "wippy-test-redis"
	testPort      = "16379"
)

var testAddr string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping Redis integration tests in short mode")
		os.Exit(0)
	}

	if os.Getenv("REDIS_ADDR") != "" {
		testAddr = os.Getenv("REDIS_ADDR")
		os.Exit(m.Run())
	}

	if !drivertest.DockerAvailable() {
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

	if !drivertest.WaitForPort(testAddr, 30*time.Second) {
		fmt.Println("redis did not become ready in time")
		_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
		os.Exit(1)
	}

	code := m.Run()

	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
	os.Exit(code)
}

func setupDriver(t *testing.T) *Driver {
	t.Helper()
	logger := zaptest.NewLogger(t)
	opts := &goredis.UniversalOptions{Addrs: []string{testAddr}}
	tc := systempayload.NewTranscoder()
	sysjson.Register(tc)
	driver := NewDriver(registry.ParseID("test:redis"), opts, tc, logger)

	ctx := context.Background()
	statusCh, err := driver.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusCh)

	t.Cleanup(func() {
		_ = driver.Stop(ctx)
	})

	return driver
}

// TestRedisDriver_Conformance runs the shared driver conformance suite.
func TestRedisDriver_Conformance(t *testing.T) {
	driver := setupDriver(t)
	drivertest.New(t, driver,
		drivertest.WithTimeout(10*time.Second),
		drivertest.WithNackRedelivers(false),
		drivertest.WithSupportsReattach(false),
	).Run()
}
