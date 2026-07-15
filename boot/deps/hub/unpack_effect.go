// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// stagedModuleDirectory is private to one directive expansion. Planning reads
// entries from stagingDir; targetDir is not changed until Prepare.
type stagedModuleDirectory struct {
	module     string
	stagingDir string
	targetDir  string
}

type unpackPlan struct {
	staged []stagedModuleDirectory
}

func (p *unpackPlan) add(staged *stagedModuleDirectory) {
	if p == nil || staged == nil {
		return
	}
	p.staged = append(p.staged, *staged)
}

func (p *unpackPlan) cleanup() error {
	if p == nil {
		return nil
	}
	var errs []error
	for _, staged := range p.staged {
		if err := os.RemoveAll(staged.stagingDir); err != nil {
			errs = append(errs, fmt.Errorf("remove staged module %s: %w", staged.module, err))
		}
	}
	p.staged = nil
	return errors.Join(errs...)
}

func (p *unpackPlan) take() []stagedModuleDirectory {
	if p == nil {
		return nil
	}
	staged := p.staged
	p.staged = nil
	return staged
}

type activatedModuleDirectory struct {
	stagedModuleDirectory
	backupDir  string
	discardDir string
}

type filesystemEffectState uint8

const (
	filesystemEffectPlanned filesystemEffectState = iota
	filesystemEffectPrepared
	filesystemEffectCommitted
	filesystemEffectRollbackPending
	filesystemEffectRolledBack
	filesystemEffectFinalized
)

type moduleFilesystemOps struct {
	rename    func(string, string) error
	removeAll func(string) error
}

func (ops moduleFilesystemOps) withDefaults() moduleFilesystemOps {
	if ops.rename == nil {
		ops.rename = os.Rename
	}
	if ops.removeAll == nil {
		ops.removeAll = os.RemoveAll
	}
	return ops
}

// moduleFilesystemEffect activates unpacked module trees and publishes their
// source roots as one transaction. Backups are retained until Finalize, after
// registry history is durable.
type moduleFilesystemEffect struct {
	ops       moduleFilesystemOps
	roots     *sourceRootEffect
	staged    []stagedModuleDirectory
	activated []activatedModuleDirectory
	mu        sync.Mutex
	state     filesystemEffectState
}

func (h *DependencyHandler) buildModuleFilesystemEffect(
	resolved []ResolvedModule,
	controlled map[string]struct{},
	plan *unpackPlan,
) (regapi.Effect, error) {
	roots, err := h.buildSourceRootEffect(resolved, controlled)
	if err != nil {
		return nil, err
	}
	if plan == nil || len(plan.staged) == 0 {
		if roots == nil {
			return nil, nil
		}
		return roots, nil
	}
	return &moduleFilesystemEffect{
		staged: plan.take(),
		roots:  roots,
	}, nil
}

func (h *DependencyHandler) hasCurrentUnpackedModule(mod ResolvedModule) bool {
	if !h.shouldUnpackModules() || mod.Source == moduleSourceReplacementTreeV1 {
		return true
	}
	name := graph.Name{Organization: mod.Org, Module: mod.Name}
	targetDir, err := containedPath(h.vendorDir, lock.ModulePath(name))
	if err != nil {
		return false
	}
	return verifyExtractedModule(targetDir, mod.Digest, mod.SizeBytes) == nil
}

func (e *moduleFilesystemEffect) Prepare(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == filesystemEffectPrepared || e.state == filesystemEffectCommitted {
		return nil
	}
	if e.state != filesystemEffectPlanned {
		return fmt.Errorf("prepare module filesystem effect in state %d", e.state)
	}

	ops := e.ops.withDefaults()
	for i, staged := range e.staged {
		activated, err := activateModuleDirectory(staged, ops)
		if err != nil {
			if activated.backupDir != "" || activated.discardDir != "" {
				e.activated = append(e.activated, activated)
			}
			remaining := e.staged[i:]
			cleanupErr := cleanupStagedModuleDirectories(remaining, ops.removeAll)
			if cleanupErr == nil {
				e.staged = nil
			} else {
				e.staged = remaining
			}
			rollbackErr := e.restoreActivated(ops)
			if cleanupErr == nil && rollbackErr == nil {
				e.state = filesystemEffectRolledBack
			} else {
				e.state = filesystemEffectRollbackPending
			}
			return errors.Join(err, cleanupErr, rollbackErr)
		}
		e.activated = append(e.activated, activated)
	}
	e.staged = nil

	if e.roots != nil {
		if err := e.roots.Prepare(ctx); err != nil {
			rollbackErr := e.restoreActivated(ops)
			if rollbackErr == nil {
				e.state = filesystemEffectRolledBack
			} else {
				e.state = filesystemEffectRollbackPending
			}
			return errors.Join(err, rollbackErr)
		}
	}
	e.state = filesystemEffectPrepared
	return nil
}

func (e *moduleFilesystemEffect) Commit(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == filesystemEffectCommitted {
		return nil
	}
	if e.state != filesystemEffectPrepared {
		return fmt.Errorf("commit module filesystem effect in state %d", e.state)
	}
	e.state = filesystemEffectCommitted
	return nil
}

func (e *moduleFilesystemEffect) Rollback(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == filesystemEffectRolledBack {
		return nil
	}
	if e.state == filesystemEffectFinalized {
		return fmt.Errorf("rollback finalized module filesystem effect")
	}
	var errs []error
	ops := e.ops.withDefaults()
	if e.state == filesystemEffectPlanned {
		if err := cleanupStagedModuleDirectories(e.staged, ops.removeAll); err != nil {
			errs = append(errs, err)
		} else {
			e.staged = nil
		}
	} else {
		errs = append(errs, e.restoreActivated(ops))
		if len(e.staged) > 0 {
			if err := cleanupStagedModuleDirectories(e.staged, ops.removeAll); err != nil {
				errs = append(errs, err)
			} else {
				e.staged = nil
			}
		}
	}
	if e.roots != nil {
		errs = append(errs, e.roots.Rollback(ctx))
	}
	err := errors.Join(errs...)
	if err == nil {
		e.state = filesystemEffectRolledBack
	} else {
		e.state = filesystemEffectRollbackPending
	}
	return err
}

func (e *moduleFilesystemEffect) Finalize(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == filesystemEffectFinalized {
		return nil
	}
	if e.state != filesystemEffectPrepared && e.state != filesystemEffectCommitted {
		return fmt.Errorf("finalize module filesystem effect in state %d", e.state)
	}
	var errs []error
	remaining := make([]activatedModuleDirectory, 0, len(e.activated))
	ops := e.ops.withDefaults()
	for _, activated := range e.activated {
		if activated.backupDir == "" {
			continue
		}
		if err := ops.removeAll(activated.backupDir); err != nil {
			errs = append(errs, fmt.Errorf("remove module backup %s: %w", activated.module, err))
			remaining = append(remaining, activated)
		}
	}
	e.activated = remaining
	err := errors.Join(errs...)
	if err == nil {
		e.state = filesystemEffectFinalized
	}
	return err
}

func (e *moduleFilesystemEffect) restoreActivated(ops moduleFilesystemOps) error {
	var errs []error
	failed := make([]activatedModuleDirectory, 0, len(e.activated))
	for i := len(e.activated) - 1; i >= 0; i-- {
		activated := e.activated[i]
		if err := restoreModuleDirectory(&activated, ops); err != nil {
			errs = append(errs, err)
			failed = append(failed, activated)
		}
	}
	for left, right := 0, len(failed)-1; left < right; left, right = left+1, right-1 {
		failed[left], failed[right] = failed[right], failed[left]
	}
	e.activated = failed
	return errors.Join(errs...)
}

func activateModuleDirectory(staged stagedModuleDirectory, ops moduleFilesystemOps) (activatedModuleDirectory, error) {
	activated := activatedModuleDirectory{stagedModuleDirectory: staged}
	parent := filepath.Dir(staged.targetDir)
	if filepath.Dir(staged.stagingDir) != parent {
		return activated, fmt.Errorf("staged module %s is not beside its target", staged.module)
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return activated, fmt.Errorf("create module parent %s: %w", staged.module, err)
	}

	if _, err := os.Lstat(staged.targetDir); err == nil {
		backupDir, backupErr := reserveSiblingPath(parent, "."+filepath.Base(staged.targetDir)+".backup-*")
		if backupErr != nil {
			return activated, fmt.Errorf("reserve module backup %s: %w", staged.module, backupErr)
		}
		if err := ops.rename(staged.targetDir, backupDir); err != nil {
			return activated, fmt.Errorf("move active module %s to backup: %w", staged.module, err)
		}
		activated.backupDir = backupDir
	} else if !errors.Is(err, os.ErrNotExist) {
		return activated, fmt.Errorf("inspect active module %s: %w", staged.module, err)
	}

	if err := ops.rename(staged.stagingDir, staged.targetDir); err != nil {
		if activated.backupDir != "" {
			if restoreErr := ops.rename(activated.backupDir, staged.targetDir); restoreErr != nil {
				return activated, errors.Join(
					fmt.Errorf("activate staged module %s: %w", staged.module, err),
					fmt.Errorf("restore module %s after activation failure: %w", staged.module, restoreErr),
				)
			}
			activated.backupDir = ""
		}
		return activated, fmt.Errorf("activate staged module %s: %w", staged.module, err)
	}
	return activated, nil
}

func restoreModuleDirectory(activated *activatedModuleDirectory, ops moduleFilesystemOps) error {
	if activated == nil {
		return nil
	}
	if activated.discardDir != "" && activated.backupDir == "" {
		if err := ops.removeAll(activated.discardDir); err != nil {
			return fmt.Errorf("remove rolled-back module %s: %w", activated.module, err)
		}
		activated.discardDir = ""
		return nil
	}
	if _, err := os.Lstat(activated.targetDir); errors.Is(err, os.ErrNotExist) {
		if activated.backupDir != "" {
			if err := ops.rename(activated.backupDir, activated.targetDir); err != nil {
				return fmt.Errorf("restore missing previous module %s: %w", activated.module, err)
			}
			activated.backupDir = ""
		}
		if activated.discardDir != "" {
			if err := ops.removeAll(activated.discardDir); err != nil {
				return fmt.Errorf("remove rolled-back module %s: %w", activated.module, err)
			}
			activated.discardDir = ""
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect active module %s during rollback: %w", activated.module, err)
	}

	parent := filepath.Dir(activated.targetDir)
	discard, err := reserveSiblingPath(parent, "."+filepath.Base(activated.targetDir)+".discard-*")
	if err != nil {
		return fmt.Errorf("reserve rollback path for %s: %w", activated.module, err)
	}
	if err := ops.rename(activated.targetDir, discard); err != nil {
		return fmt.Errorf("move staged module %s out of service: %w", activated.module, err)
	}
	activated.discardDir = discard

	if activated.backupDir != "" {
		if err := ops.rename(activated.backupDir, activated.targetDir); err != nil {
			restoreErr := ops.rename(discard, activated.targetDir)
			if restoreErr == nil {
				activated.discardDir = ""
			}
			return errors.Join(
				fmt.Errorf("restore previous module %s: %w", activated.module, err),
				wrapRollbackRestoreError(activated.module, restoreErr),
			)
		}
		activated.backupDir = ""
	}
	if err := ops.removeAll(discard); err != nil {
		return fmt.Errorf("remove rolled-back module %s: %w", activated.module, err)
	}
	activated.discardDir = ""
	return nil
}

func wrapRollbackRestoreError(module string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("put staged module %s back after restore failure: %w", module, err)
}

func reserveSiblingPath(parent, pattern string) (string, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		_ = os.RemoveAll(path)
		return "", err
	}
	return path, nil
}

func cleanupStagedModuleDirectories(staged []stagedModuleDirectory, removeAll func(string) error) error {
	var errs []error
	for _, module := range staged {
		if err := removeAll(module.stagingDir); err != nil {
			errs = append(errs, fmt.Errorf("remove staged module %s: %w", module.module, err))
		}
	}
	return errors.Join(errs...)
}
