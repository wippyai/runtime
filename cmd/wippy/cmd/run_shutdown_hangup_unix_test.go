// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// A closed terminal hangs up the run; the run stops gracefully as it does on
// SIGTERM, so its services stop the processes they started.
func TestSetupShutdownSources_StopsOnHangup(t *testing.T) {
	sources := setupShutdownSources(t.Context())
	defer signal.Stop(sources.signals)

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}

	select {
	case sig := <-sources.signals:
		if sig != syscall.SIGHUP {
			t.Fatalf("shutdown source received %v, want SIGHUP", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGHUP did not reach the shutdown sources")
	}
}
