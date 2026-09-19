// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// commandRunner runs the executable again with args, in the working directory
// dir. Tests replace it to drive an update without a child process.
type commandRunner func(ctx context.Context, dir string, args []string) error

func runChild(ctx context.Context, dir string, args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return NewApplicationStateError("resolve running executable", "", err)
	}
	// The child runs the same binary, which is the only source of the path.
	command := exec.CommandContext(ctx, executable, args...) //nolint:gosec // executable comes only from os.Executable
	command.Dir = dir
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

// updateDeployment moves the deployment forward in a candidate copy and selects
// it only after the Wippy CLI has updated it, linted it against this executable
// and every module artifact it pins has verified. The candidate is built inside
// a state of its own, so the child invocations that carry out the update own
// that state while this invocation keeps the lock on the application state.
// Any failure removes the candidate and leaves the current deployment selected.
func updateDeployment(ctx context.Context, e Executable, l Launch, deployment string, run commandRunner) (result error) {
	deployments := deploymentsPath(l.State)
	name, err := nextUpdateName(deployments)
	if err != nil {
		return err
	}
	staging := filepath.Join(deployments, updateStagingPrefix+strings.TrimPrefix(name, updatePrefix))
	candidate := filepath.Join(staging, deploymentsDir, e.Bundle.ID())
	selected := filepath.Join(deployments, name)
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(selected)
		}
		_ = os.RemoveAll(staging)
	}()
	if err := os.CopyFS(candidate, os.DirFS(deployment)); err != nil {
		return NewUpdateError("stage", err)
	}
	// The configuration keeps its original path so relative replacements
	// resolve against the base the application state declares.
	child := []string{"--state", staging, OpWippy.String()}
	files, err := configFiles(l.State)
	if err != nil {
		return err
	}
	for _, file := range files {
		child = append(child, "--config", file)
	}
	if err := run(ctx, candidate, append(append([]string{}, child...), append([]string{"update"}, l.Args...)...)); err != nil {
		return NewUpdateError("update", err)
	}
	if err := run(ctx, candidate, append(append([]string{}, child...), "lint")); err != nil {
		return NewUpdateError("lint", err)
	}
	if err := verifyCandidate(e, candidate); err != nil {
		return err
	}
	if err := os.Rename(candidate, selected); err != nil {
		return NewUpdateError("select", err)
	}
	previous, err := filepath.Rel(l.State, deployment)
	if err != nil {
		return NewUpdateError("select", err)
	}
	record, err := json.Marshal(current{
		Directory: deploymentsDir + "/" + name,
		Base:      e.Bundle.ID(),
		Previous:  filepath.ToSlash(previous),
	})
	if err != nil {
		return NewUpdateError("select", err)
	}
	if err := writeRecord(currentPath(l.State), record); err != nil {
		return err
	}
	committed = true
	return nil
}

// verifyCandidate accepts a candidate only when it still selects this
// executable's application and every module it pins is a regular file whose
// content carries the digest the lock records.
func verifyCandidate(e Executable, candidate string) error {
	path, err := e.Bundle.existing(filepath.Join(candidate, lock.DefaultFilename))
	if err != nil {
		return NewUpdateError("verify", err)
	}
	locked, err := lock.New(path)
	if err != nil {
		return NewUpdateError("verify", err)
	}
	for _, module := range locked.GetModuleLoadPaths() {
		if module.Module == "" {
			continue
		}
		if info, err := os.Stat(module.Path); err != nil || !info.Mode().IsRegular() {
			return NewUpdatedModuleError("is not a verified pack", module.Module, err)
		}
		if module.Digest == "" {
			return NewUpdatedModuleError("has no digest", module.Module, nil)
		}
		if err := hub.VerifyDownloadedArtifact(module.Path, module.Digest, 0); err != nil {
			return NewUpdatedModuleError("does not carry the content its digest names", module.Module, err)
		}
	}
	return nil
}

// nextUpdateName names the candidate directory this update produces. Numbering
// increases across the updates a state holds, so a deployment directory names
// its place in the order the state moved through.
func nextUpdateName(deployments string) (string, error) {
	entries, err := os.ReadDir(deployments)
	if err != nil && !os.IsNotExist(err) {
		return "", NewApplicationStateError("read deployments", deployments, err)
	}
	highest := 0
	for _, entry := range entries {
		number, found := strings.CutPrefix(entry.Name(), updatePrefix)
		if !found {
			continue
		}
		if value, err := strconv.Atoi(number); err == nil && value > highest {
			highest = value
		}
	}
	return updatePrefix + strconv.Itoa(highest+1), nil
}
