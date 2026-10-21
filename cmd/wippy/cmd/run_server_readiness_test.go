// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/wapp"
	"gopkg.in/yaml.v3"
)

func TestRunServerReadinessHelper(t *testing.T) {
	mode := os.Getenv("WIPPY_SERVER_READINESS_HELPER")
	if mode == "" {
		return
	}
	setTestConfigFiles(t, "wippy.yaml")
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	cmd.Flags().Bool("verbose", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--verbose"}))
	cmd.SetContext(context.Background())
	var args []string
	if mode == "pack" {
		args = []string{"app.wapp"}
	}
	if err := runWithUseCase(cmd, args, defaultUseCase); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func TestRunServerBootReadiness(t *testing.T) {
	for _, mode := range []string{"workspace", "pack"} {
		for _, outcome := range []string{"failure", "success", "no_gates", "pending"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				workDir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(workDir, "wippy.yaml"), []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
				entries := []wapp.Entry{{ID: wapp.NewID("app", "workers"), Kind: "process.host", Data: map[string]any{
					"host": map[string]any{"workers": 2}, "lifecycle": map[string]any{"auto_start": true},
				}}}
				if outcome != "no_gates" {
					body := `print("BOOT_SUCCEEDED")`
					if outcome == "failure" {
						body = `return nil, "deliberate initialization failure"`
					} else if outcome == "pending" {
						body = `print("BOOT_PENDING"); time.after("1h"):receive()`
					}
					entries = append(entries,
						wapp.Entry{ID: wapp.NewID("app", "boot"), Kind: "process.lua", Data: map[string]any{
							"method": "main", "modules": []string{"time"},
							"source": `local time = require("time"); return {main = function() ` + body + ` end}`,
						}},
						wapp.Entry{ID: wapp.NewID("app", "boot_service"), Kind: "process.service", Data: map[string]any{
							"process": "app:boot", "host": "app:workers",
							"lifecycle": map[string]any{"auto_start": true, "startup": "complete"},
						}},
					)
				}
				if mode == "pack" {
					createTestPackFile(t, workDir, "app", entries)
				} else {
					require.NoError(t, os.Mkdir(filepath.Join(workDir, "src"), 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(workDir, "wippy.lock"), []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
					var declarations []map[string]any
					for _, entry := range entries {
						declaration := map[string]any{"name": entry.ID.Name, "kind": entry.Kind}
						for key, value := range entry.Data.(map[string]any) {
							declaration[key] = value
						}
						declarations = append(declarations, declaration)
					}
					data, err := yaml.Marshal(map[string]any{"version": "1.0", "namespace": "app", "entries": declarations})
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filepath.Join(workDir, "src", "_index.yaml"), data, 0o600))
				}

				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunServerReadinessHelper$")
				child.Dir = workDir
				child.Env = append(os.Environ(), "WIPPY_SERVER_READINESS_HELPER="+mode)
				output, err := child.StdoutPipe()
				require.NoError(t, err)
				child.Stderr = child.Stdout
				require.NoError(t, child.Start())
				var log strings.Builder
				marker := "runtime ready"
				if outcome == "success" {
					marker = "BOOT_SUCCEEDED"
				} else if outcome == "pending" {
					marker = "BOOT_PENDING"
				}
				ready := make(chan struct{})
				done := make(chan error, 1)
				go func() {
					scanner := bufio.NewScanner(output)
					marked := false
					for scanner.Scan() {
						line := scanner.Text()
						log.WriteString(line + "\n")
						if !marked && strings.Contains(line, marker) {
							marked = true
							close(ready)
						}
					}
					scanErr := scanner.Err()
					waitErr := child.Wait()
					if scanErr != nil {
						done <- scanErr
					} else {
						done <- waitErr
					}
				}()
				if outcome == "failure" {
					err := <-done
					require.NoError(t, ctx.Err(), "runtime remains alive after boot failure: %s", log.String())
					var exitErr *exec.ExitError
					require.ErrorAs(t, err, &exitErr, "output: %s", log.String())
					require.Equal(t, 1, exitErr.ExitCode(), "output: %s", log.String())
					require.Contains(t, log.String(), "boot readiness failed")
					require.Contains(t, log.String(), "deliberate initialization failure")
					return
				}
				select {
				case <-ready:
				case err := <-done:
					t.Fatalf("server exits before readiness: %v\n%s", err, log.String())
				case <-ctx.Done():
					<-done
					t.Fatalf("server does not reach readiness: %s", log.String())
				}
				select {
				case err := <-done:
					t.Fatalf("server exits without shutdown: %v\n%s", err, log.String())
				case <-time.After(300 * time.Millisecond):
				}
				require.NoError(t, child.Process.Signal(syscall.SIGTERM))
				require.NoError(t, <-done, "output: %s", log.String())
				require.NoError(t, ctx.Err(), "shutdown stalls: %s", log.String())
				if outcome == "success" {
					require.Contains(t, log.String(), "BOOT_SUCCEEDED")
				}
			})
		}
	}
}
