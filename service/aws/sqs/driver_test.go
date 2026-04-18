// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	"github.com/wippyai/runtime/service/queue/drivertest"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap/zaptest"
)

const (
	testContainer = "wippy-test-elasticmq"
	testPort      = "19324"
)

var testEndpoint string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping SQS integration tests in short mode")
		os.Exit(0)
	}

	if os.Getenv("SQS_ENDPOINT") != "" {
		testEndpoint = os.Getenv("SQS_ENDPOINT")
		os.Exit(m.Run())
	}

	if !drivertest.DockerAvailable() {
		// Pure-unit tests don't need docker; conformance tests gated on
		// testEndpoint skip themselves when it remains empty.
		fmt.Println("docker not available; running unit tests only")
		os.Exit(m.Run())
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

	if !drivertest.WaitForPort("localhost:"+testPort, 30*time.Second) {
		fmt.Println("elasticmq did not become ready in time")
		_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
		os.Exit(1)
	}

	code := m.Run()

	_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", testContainer).Run()
	os.Exit(code)
}

func setupDriver(t *testing.T) *Driver {
	t.Helper()
	if testEndpoint == "" {
		t.Skip("SQS endpoint not available (docker or SQS_ENDPOINT required)")
	}
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

	tc := systempayload.NewTranscoder()
	sysjson.Register(tc)
	driver := NewDriver(registry.ParseID("test:sqs"), sqsCfg, awsCfg, tc, logger)

	statusCh, err := driver.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusCh)

	t.Cleanup(func() {
		_ = driver.Stop(ctx)
	})

	return driver
}

// TestSQSDriver_Conformance runs the shared driver conformance suite.
func TestSQSDriver_Conformance(t *testing.T) {
	driver := setupDriver(t)
	drivertest.New(t, driver,
		drivertest.WithTimeout(30*time.Second),
		drivertest.WithPreservesMessageID(false),
		drivertest.WithGetQueueInfoAccurate(false),
		drivertest.WithSupportsReattach(false),
	).Run()
}
