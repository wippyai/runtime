// SPDX-License-Identifier: MPL-2.0
//go:build !windows

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

// A CLI dependency previously queried OSC 11 from init(), before main could
// start the runtime. A terminal that never answers must still reach command code.
func TestCLIStartupWithoutTerminalResponses(t *testing.T) {
	if os.Getenv("WIPPY_TEST_STARTUP_CHILD") == "1" {
		fmt.Println("command code reached")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIStartupWithoutTerminalResponses$")
	cmd.Env = append(os.Environ(), "WIPPY_TEST_STARTUP_CHILD=1", "TERM=xterm-256color")
	terminal, err := pty.Start(cmd)
	require.NoError(t, err)
	defer terminal.Close()
	// Deliberately send no bytes to the controlling terminal.
	err = cmd.Wait()
	require.NoError(t, ctx.Err(), "startup waited for terminal query responses")
	require.NoError(t, err)
}
