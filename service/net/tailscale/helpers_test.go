// SPDX-License-Identifier: MPL-2.0

package tailscale

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/wippyai/runtime/service/net/nettest"
)

const (
	// defaultHeadscaleContainer is the Docker container name for Headscale.
	defaultHeadscaleContainer = "overlay-networks-app-headscale-1"

	// defaultHeadscaleUser is the Headscale user for creating preauthkeys.
	defaultHeadscaleUser = "wippy-test"

	// defaultHeadscaleControlURL is the default Headscale control URL when
	// auto-generating auth keys from the local Docker instance.
	defaultHeadscaleControlURL = "http://localhost:8090"
)

// execCommandContext is a variable so tests could override it if needed.
var execCommandContext = exec.CommandContext

// headscaleAuthKey attempts to generate a preauthkey from a running
// Headscale Docker container. Returns the key or an error if Docker is
// not available or the container is not running.
//
// Container name and user can be overridden via HEADSCALE_CONTAINER and
// HEADSCALE_USER environment variables.
func headscaleAuthKey() (string, error) {
	container := os.Getenv("HEADSCALE_CONTAINER")
	if container == "" {
		container = defaultHeadscaleContainer
	}
	user := os.Getenv("HEADSCALE_USER")
	if user == "" {
		user = defaultHeadscaleUser
	}

	out, err := execCommand("docker", "exec", container,
		"headscale", "preauthkeys", "create",
		"--user", user,
		"--reusable",
		"--expiration", "1h",
	)
	if err != nil {
		return "", fmt.Errorf("headscale preauthkey creation failed: %w (output: %s)", err, out)
	}

	key := nettest.LastNonEmptyLine(out)
	if key == "" {
		return "", fmt.Errorf("headscale returned empty preauthkey (raw output: %q)", out)
	}

	return key, nil
}

// execCommand runs an external command and returns its combined output.
func execCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tailscaleEnv returns the Tailscale auth key and control URL for E2E tests.
//
// Resolution order for auth key:
//  1. TS_AUTHKEY environment variable (explicit key)
//  2. Auto-generate from running Headscale Docker container
//
// When auto-generating, TS_CONTROL_URL defaults to http://localhost:8090
// (the standard Headscale docker-compose port mapping).
func tailscaleEnv() (authKey, controlURL string) {
	authKey = os.Getenv("TS_AUTHKEY")
	controlURL = os.Getenv("TS_CONTROL_URL")

	if authKey != "" {
		return authKey, controlURL
	}

	key, err := headscaleAuthKey()
	if err != nil {
		return "", controlURL
	}

	authKey = key
	if controlURL == "" {
		controlURL = defaultHeadscaleControlURL
	}

	return authKey, controlURL
}

// skipIfNoTailscale skips the test if Tailscale credentials are not
// available. Unlike SOCKS5/I2P which just need a proxy reachable, Tailscale
// needs an auth key and a control server (tailscale.com or Headscale).
//
// When TS_AUTHKEY is not set, this function attempts to auto-generate a
// preauthkey from a running Headscale Docker container before skipping.
func skipIfNoTailscale(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("Tailscale E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	authKey, controlURL := tailscaleEnv()
	if authKey == "" {
		t.Skip("Tailscale E2E tests require TS_AUTHKEY or a running Headscale Docker container")
	}

	t.Logf("Tailscale auth key available (control=%s)", controlURL)
}

// errorAs walks the error chain looking for a match that As can extract.
func errorAs(err error, target any) bool { return nettest.ErrorAs(err, target) }

// containsAny reports whether any of substrs is a substring of s.
func containsAny(s string, substrs ...string) bool { return nettest.ContainsAny(s, substrs...) }
