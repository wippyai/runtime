// SPDX-License-Identifier: MPL-2.0

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
)

type activation struct {
	Directory string `json:"directory"`
	Previous  string `json:"previous,omitempty"`
}

func selectedDeployment(state string) (string, error) {
	data, err := os.ReadFile(filepath.Join(state, "active.json"))
	if os.IsNotExist(err) {
		return filepath.Join(state, "deployment"), nil
	}
	if err != nil {
		return "", err
	}
	var current activation
	if err := json.Unmarshal(data, &current); err != nil {
		return "", fmt.Errorf("read application activation: %w", err)
	}
	if !filepath.IsLocal(current.Directory) || !strings.HasPrefix(filepath.ToSlash(current.Directory), "revisions/") {
		return "", fmt.Errorf("invalid application activation path")
	}
	return filepath.Join(state, current.Directory), nil
}

func lockApplication(state string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(state, ".application.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	unlock, err := tryLockFile(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("application is already running or being updated: %w", err)
	}
	return func() { _ = unlock(); _ = file.Close() }, nil
}

type commandRunner func(context.Context, string, ...string) error

func runChild(ctx context.Context, state string, args ...string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Reexecute the running binary with the supplied argument slice.
	command := exec.CommandContext(ctx, executable, append([]string{"--state-dir", state, "runtime"}, args...)...) //nolint:gosec // executable comes only from os.Executable
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

// updateDeployment runs Wippy update and lint in a disposable deployment.
// Activation changes only after both commands and artifact verification succeed.
func updateDeployment(ctx context.Context, options Options, state, current string, args []string, run commandRunner) error {
	revisions := filepath.Join(state, "revisions")
	if err := os.MkdirAll(revisions, 0o700); err != nil {
		return err
	}
	candidate, err := os.MkdirTemp(revisions, "revision-")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(candidate)
		}
	}()
	deployment := filepath.Join(candidate, "deployment")
	if err := os.CopyFS(deployment, os.DirFS(current)); err != nil {
		return fmt.Errorf("stage application update: %w", err)
	}
	// Keep the original config path so relative replacements retain their base.
	prefix := []string{}
	config := filepath.Join(state, ".wippy.yaml")
	if _, err := os.Stat(config); err == nil {
		prefix = []string{"--config", config}
	} else if !os.IsNotExist(err) {
		return err
	}
	updateArgs := append(append([]string{}, prefix...), "update")
	if err := run(ctx, candidate, append(updateArgs, args...)...); err != nil {
		return fmt.Errorf("update failed; active deployment unchanged: %w", err)
	}
	if err := run(ctx, candidate, append(append([]string{}, prefix...), "lint")...); err != nil {
		return fmt.Errorf("updated application is incompatible with this executable: %w", err)
	}
	path, err := options.Bundle.existing(filepath.Join(deployment, lock.DefaultFilename))
	if err != nil {
		return err
	}
	locked, err := lock.New(path)
	if err != nil {
		return err
	}
	for _, pack := range locked.GetModuleLoadPaths() {
		if pack.Module == "" {
			continue
		}
		if info, err := os.Stat(pack.Path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("updated module %s must be a verified pack", pack.Module)
		}
		if pack.Digest == "" {
			return fmt.Errorf("updated module %s has no digest", pack.Module)
		}
		if err := hub.VerifyDownloadedArtifact(pack.Path, pack.Digest, 0); err != nil {
			return err
		}
	}
	relative, err := filepath.Rel(state, deployment)
	if err != nil {
		return err
	}
	previous, err := filepath.Rel(state, current)
	if err != nil {
		return err
	}
	data, err := json.Marshal(activation{Directory: relative, Previous: previous})
	if err != nil {
		return err
	}
	pending, err := os.CreateTemp(state, ".activation-")
	if err != nil {
		return err
	}
	pendingPath := pending.Name()
	defer os.Remove(pendingPath)
	if _, err := pending.Write(data); err != nil {
		_ = pending.Close()
		return err
	}
	if err := pending.Sync(); err != nil {
		_ = pending.Close()
		return err
	}
	if err := pending.Close(); err != nil {
		return err
	}
	if err := os.Rename(pendingPath, filepath.Join(state, "active.json")); err != nil {
		return err
	}
	committed = true
	return nil
}
