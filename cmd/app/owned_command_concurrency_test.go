// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

func TestOwnedCommandConcurrentLaunchHelper(t *testing.T) {
	state := os.Getenv("WIPPY_TEST_CONCURRENT_OWNED_STATE")
	if state == "" {
		return
	}
	input := bufio.NewScanner(os.Stdin)
	executable := runnableExecutable(t)
	executable.OwnedCommand = "client"
	executable.Host = &plannedHost{before: func() {
		fmt.Println("OWNED_LAUNCH_READY")
		require.True(t, input.Scan())
		require.Equal(t, "start", input.Text())
	}}
	previous := execute
	t.Cleanup(func() { execute = previous })
	execute = func(_ context.Context, options cmd.ExecuteOptions) error {
		command := options.Args[3]
		fmt.Println("OWNED_LAUNCH_" + command)
		if command == "desktop" {
			// Keep the real lock until every competing invocation has selected
			// its command. Each child has independent CLI and environment state.
			require.True(t, input.Scan())
			require.Equal(t, "release", input.Text())
		} else {
			require.Equal(t, "client", command)
		}
		return nil
	}
	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "run"}))
}

func TestOwnedCommandConcurrentLaunchesChooseOneOwner(t *testing.T) {
	const count = 8
	state := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	var children sync.WaitGroup
	var inputs []io.WriteCloser
	t.Cleanup(func() {
		cancel()
		for _, input := range inputs {
			_ = input.Close()
		}
		children.Wait()
	})
	type event struct {
		err  error
		text string
		id   int
		done bool
	}
	events := make(chan event, 4*count)
	for id := range count {
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnedCommandConcurrentLaunchHelper$")
		child.Env = append(os.Environ(), "WIPPY_TEST_CONCURRENT_OWNED_STATE="+state)
		output, err := child.StdoutPipe()
		require.NoError(t, err)
		input, err := child.StdinPipe()
		require.NoError(t, err)
		inputs = append(inputs, input)
		stderr := new(bytes.Buffer)
		child.Stderr = stderr
		require.NoError(t, child.Start())
		children.Add(1)
		go func() {
			defer children.Done()
			scanner := bufio.NewScanner(output)
			var log strings.Builder
			for scanner.Scan() {
				line := scanner.Text()
				log.WriteString(line + "\n")
				if strings.HasPrefix(line, "OWNED_LAUNCH_") {
					events <- event{id: id, text: line}
				}
			}
			err := child.Wait()
			if err == nil {
				err = scanner.Err()
			}
			if err != nil {
				err = fmt.Errorf("child %d: %w\n%s%s", id, err, log.String(), stderr.String())
			}
			events <- event{id: id, done: true, err: err}
		}()
	}
	receive := func() event {
		select {
		case next := <-events:
			require.NoError(t, next.err)
			return next
		case <-ctx.Done():
			t.Fatal("concurrent launches did not finish: ", ctx.Err())
			return event{}
		}
	}
	for range count {
		next := receive()
		require.False(t, next.done)
		require.Equal(t, "OWNED_LAUNCH_READY", next.text)
	}
	for _, input := range inputs {
		_, err := fmt.Fprintln(input, "start")
		require.NoError(t, err)
	}
	owner, owners, clients, finished := -1, 0, 0, 0
	for owners+clients < count {
		next := receive()
		if next.done {
			finished++
			continue
		}
		switch next.text {
		case "OWNED_LAUNCH_desktop":
			owner = next.id
			owners++
		case "OWNED_LAUNCH_client":
			clients++
		default:
			t.Fatalf("unexpected child output: %s", next.text)
		}
	}
	require.Equal(t, 1, owners)
	require.Equal(t, count-1, clients)
	_, err := fmt.Fprintln(inputs[owner], "release")
	require.NoError(t, err)
	for finished < count {
		require.True(t, receive().done)
		finished++
	}
	owned, err := Owned(state)
	require.NoError(t, err)
	require.False(t, owned, "owner exit must release the state lock")
}
