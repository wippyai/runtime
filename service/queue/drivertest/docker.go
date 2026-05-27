// SPDX-License-Identifier: MPL-2.0

// Package drivertest provides a conformance test harness for queue.Driver implementations.
//
// Each queue driver (memory, AMQP, Redis, SQS, etc.) can use this package to verify
// it correctly implements the queue.Driver interface contract. The harness runs a
// standard set of tests covering DeclareQueue, Publish, Attach, Nack, GetQueueInfo,
// and error handling for non-existent queues.
package drivertest

import (
	"context"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DockerAvailable reports whether a working Docker daemon is accessible.
// On Windows it additionally checks that Docker is running in Linux container mode,
// which is required for the test container images.
func DockerAvailable() bool {
	if runtime.GOOS == "windows" {
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

// WaitForPort polls a TCP address until a connection succeeds or the timeout elapses.
// Returns true if the port became reachable within the timeout.
func WaitForPort(addr string, timeout time.Duration) bool {
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
