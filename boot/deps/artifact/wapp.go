// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wippyai/wapp"
)

// WAPP identifies one already-selected module pack. Selection, download, and
// integrity verification belong to the caller.
type WAPP struct {
	Path          string
	ModuleVersion string
}

// Resource identifies an already-resolved filesystem resource. It is used for
// active local module replacements, where no WAPP transport is involved.
type Resource struct {
	Filesystem    fs.FS
	Meta          wapp.Metadata
	ModuleVersion string
	ResourceID    wapp.ID
	Source        string
}

// Materialized describes one artifact output owned by a WAPP resource.
type Materialized struct {
	Descriptor  Descriptor
	Destination string
	ResourceID  wapp.ID
	Source      string
}

type wappEffectState uint8

const (
	wappEffectPlanned wappEffectState = iota
	wappEffectPrepared
	wappEffectCommitted
	wappEffectRollbackPending
	wappEffectRolledBack
	wappEffectFinalized
)

type activatedRoot struct {
	destination string
	staging     string
	backup      string
	hadTarget   bool
}

// Effect materializes artifact resources as a registry-compatible
// transaction effect. It deliberately does not resolve or download modules.
type Effect struct {
	registry  *Registry
	unlock    func() error
	root      string
	packs     []WAPP
	resources []Resource
	activated []activatedRoot
	pending   []stagedRoot
	results   []Materialized
	mu        sync.Mutex
	exact     bool
	state     wappEffectState
}

// NewWAPPEffect creates a materialization effect for exact, verified WAPPs.
func NewWAPPEffect(registry *Registry, packs []WAPP, root string) (*Effect, error) {
	return NewEffect(registry, packs, nil, root)
}

// NewEffect creates a materialization effect for exact verified WAPPs and
// already-resolved local replacement resources.
func NewEffect(
	registry *Registry,
	packs []WAPP,
	resources []Resource,
	root string,
) (*Effect, error) {
	return newEffect(registry, packs, resources, root, true)
}

// NewPartialEffect overlays selected resources while preserving outputs that
// belong to modules outside a targeted install.
func NewPartialEffect(
	registry *Registry,
	packs []WAPP,
	resources []Resource,
	root string,
) (*Effect, error) {
	return newEffect(registry, packs, resources, root, false)
}

func newEffect(
	registry *Registry,
	packs []WAPP,
	resources []Resource,
	root string,
	exact bool,
) (*Effect, error) {
	if registry == nil {
		return nil, errors.New("artifact registry is nil")
	}
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact root is empty")
	}
	return &Effect{
		registry:  registry,
		packs:     append([]WAPP(nil), packs...),
		resources: append([]Resource(nil), resources...),
		root:      root,
		exact:     exact,
	}, nil
}

// Results returns a copy of the successfully prepared outputs.
func (e *Effect) Results() []Materialized {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Materialized(nil), e.results...)
}

// Prepare validates every declared artifact before activating any output, then
// retains prior outputs for rollback.
func (e *Effect) Prepare(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == wappEffectPrepared || e.state == wappEffectCommitted {
		return nil
	}
	if e.state != wappEffectPlanned {
		return fmt.Errorf("prepare artifact effect in state %d", e.state)
	}

	candidates, closePacks, err := e.inspect(ctx)
	if err != nil {
		return err
	}
	unlock, lockErr := acquireArtifactLock(ctx, e.root)
	if lockErr != nil {
		closePacks()
		return lockErr
	}
	e.unlock = unlock
	staged, stageErr := e.stage(candidates)
	closePacks()
	if stageErr != nil {
		return errors.Join(stageErr, e.releaseLock())
	}

	for i, root := range staged {
		activated, activateErr := activateRoot(root)
		if activateErr != nil {
			cleanupFrom := i
			if activated.backup != "" || activated.hadTarget {
				e.activated = append(e.activated, activated)
				cleanupFrom = i + 1
			}
			pending := staged[cleanupFrom:]
			cleanupErr := cleanupStagedRoots(pending)
			remaining, rollbackErr := rollbackActivated(e.activated)
			e.activated = remaining
			if cleanupErr == nil && rollbackErr == nil {
				e.activated = nil
				e.pending = nil
				e.results = nil
				e.state = wappEffectRolledBack
			} else {
				if cleanupErr != nil {
					e.pending = append(e.pending, pending...)
				}
				e.state = wappEffectRollbackPending
			}
			unlockErr := e.releaseLock()
			return errors.Join(activateErr, cleanupErr, rollbackErr, unlockErr)
		}
		e.activated = append(e.activated, activated)
	}
	for _, candidate := range candidates {
		e.results = append(e.results, candidate.result)
	}
	e.state = wappEffectPrepared
	return nil
}

// Commit marks the prepared outputs as committed while keeping rollback data
// until the surrounding transaction is durable.
func (e *Effect) Commit(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == wappEffectCommitted {
		return nil
	}
	if e.state != wappEffectPrepared {
		return fmt.Errorf("commit artifact effect in state %d", e.state)
	}
	e.state = wappEffectCommitted
	return nil
}

// Rollback restores every output replaced during Prepare.
func (e *Effect) Rollback(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == wappEffectRolledBack {
		return nil
	}
	if e.state == wappEffectFinalized {
		return errors.New("rollback finalized artifact effect")
	}
	if e.state == wappEffectPlanned {
		e.state = wappEffectRolledBack
		return nil
	}
	if e.state == wappEffectRollbackPending && e.unlock == nil {
		unlock, err := acquireArtifactLock(ctx, e.root)
		if err != nil {
			return err
		}
		e.unlock = unlock
	}
	remaining, rollbackErr := rollbackActivated(e.activated)
	e.activated = remaining
	cleanupErr := cleanupStagedRoots(e.pending)
	if cleanupErr == nil {
		e.pending = nil
	}
	unlockErr := e.releaseLock()
	if err := errors.Join(rollbackErr, cleanupErr, unlockErr); err != nil {
		e.state = wappEffectRollbackPending
		return err
	}
	e.activated = nil
	e.pending = nil
	e.results = nil
	e.state = wappEffectRolledBack
	return nil
}

// Finalize removes rollback data after the surrounding transaction is durable.
func (e *Effect) Finalize(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == wappEffectFinalized {
		return nil
	}
	if e.state != wappEffectPrepared && e.state != wappEffectCommitted {
		return fmt.Errorf("finalize artifact effect in state %d", e.state)
	}
	var errs []error
	remaining := make([]activatedRoot, 0)
	for _, activated := range e.activated {
		if activated.backup == "" {
			continue
		}
		if err := os.RemoveAll(activated.backup); err != nil {
			errs = append(errs, fmt.Errorf("remove artifact backup %s: %w", activated.destination, err))
			remaining = append(remaining, activated)
		}
	}
	err := errors.Join(append(errs, e.releaseLock())...)
	e.activated = remaining
	if err == nil {
		e.state = wappEffectFinalized
	}
	return err
}

func (e *Effect) releaseLock() error {
	if e.unlock == nil {
		return nil
	}
	unlock := e.unlock
	e.unlock = nil
	return unlock()
}

// MaterializeWAPPs runs the same effect lifecycle for non-registry callers.
func MaterializeWAPPs(
	ctx context.Context,
	registry *Registry,
	packs []WAPP,
	root string,
) ([]Materialized, error) {
	effect, err := NewWAPPEffect(registry, packs, root)
	if err != nil {
		return nil, err
	}
	if err := effect.Prepare(ctx); err != nil {
		return nil, err
	}
	if err := effect.Commit(ctx); err != nil {
		rollbackErr := effect.Rollback(ctx)
		return nil, errors.Join(err, rollbackErr)
	}
	results := effect.Results()
	if err := effect.Finalize(ctx); err != nil {
		return nil, err
	}
	return results, nil
}

type artifactCandidate struct {
	filesystem fs.FS
	root       string
	result     Materialized
	mutable    bool
}

type stagedRoot struct {
	destination string
	staging     string
}

func (e *Effect) inspect(ctx context.Context) ([]artifactCandidate, func(), error) {
	root, err := filepath.Abs(e.root)
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve artifact root: %w", err)
	}
	if err := ensureMaterializationRoot(root); err != nil {
		return nil, func() {}, err
	}

	var files []*os.File
	closePacks := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	var candidates []artifactCandidate
	destinations := make(map[string]wapp.ID)
	appendResource := func(
		meta wapp.Metadata,
		filesystem fs.FS,
		moduleVersion string,
		resourceID wapp.ID,
		source string,
		mutable bool,
	) error {
		declaration, declared, err := ParseDeclaration(meta)
		if err != nil {
			return fmt.Errorf("resource %s from %s: %w", resourceID.String(), source, err)
		}
		if !declared {
			return nil
		}
		if filesystem == nil {
			return fmt.Errorf("resource %s from %s has no filesystem", resourceID.String(), source)
		}
		descriptor, err := e.registry.Inspect(ctx, declaration, InspectInput{
			Filesystem:    filesystem,
			ModuleVersion: moduleVersion,
			ResourceID:    resourceID,
		})
		if err != nil {
			return err
		}
		format, _ := e.registry.Resolve(declaration.Format)
		managedRoot := pathClean(format.Root())
		destination := filepath.Join(root, filepath.FromSlash(descriptor.RelativePath))
		if err := ensureWithinRoot(root, destination); err != nil {
			return err
		}
		key := strings.ToLower(filepath.Clean(destination))
		for previousPath, previous := range destinations {
			if pathsOverlap(key, previousPath) {
				return fmt.Errorf(
					"artifact resources %s at %q and %s at %q have overlapping outputs",
					previous.String(), previousPath,
					resourceID.String(), descriptor.RelativePath,
				)
			}
		}
		destinations[key] = resourceID
		candidates = append(candidates, artifactCandidate{
			filesystem: filesystem,
			root:       managedRoot,
			mutable:    mutable,
			result: Materialized{
				Descriptor:  descriptor,
				Destination: destination,
				ResourceID:  resourceID,
				Source:      source,
			},
		})
		return nil
	}

	for _, pack := range e.packs {
		file, openErr := os.Open(pack.Path)
		if openErr != nil {
			closePacks()
			return nil, func() {}, fmt.Errorf("open WAPP %s: %w", pack.Path, openErr)
		}
		files = append(files, file)
		reader, readErr := wapp.NewReader(file)
		if readErr != nil {
			closePacks()
			return nil, func() {}, fmt.Errorf("read WAPP %s: %w", pack.Path, readErr)
		}
		moduleVersion := pack.ModuleVersion
		if moduleVersion == "" {
			metadata, metadataErr := reader.GetMetadata()
			if metadataErr != nil {
				closePacks()
				return nil, func() {}, fmt.Errorf("read WAPP metadata %s: %w", pack.Path, metadataErr)
			}
			moduleVersion, _ = metadata["version"].(string)
		}
		for _, resource := range reader.ListResources() {
			filesystem, fsErr := reader.GetFS(resource.ID)
			if fsErr != nil {
				closePacks()
				return nil, func() {}, fmt.Errorf("open resource %s in %s: %w", resource.ID.String(), pack.Path, fsErr)
			}
			if err := appendResource(
				resource.Meta, filesystem, moduleVersion, resource.ID, pack.Path, false,
			); err != nil {
				closePacks()
				return nil, func() {}, err
			}
		}
	}
	for _, resource := range e.resources {
		if err := appendResource(
			resource.Meta,
			resource.Filesystem,
			resource.ModuleVersion,
			resource.ResourceID,
			resource.Source,
			true,
		); err != nil {
			closePacks()
			return nil, func() {}, err
		}
	}
	return candidates, closePacks, nil
}

func (e *Effect) stage(candidates []artifactCandidate) ([]stagedRoot, error) {
	root, err := filepath.Abs(e.root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	if err := ensureMaterializationRoot(root); err != nil {
		return nil, err
	}
	byRoot := make(map[string][]artifactCandidate)
	for _, candidate := range candidates {
		byRoot[candidate.root] = append(byRoot[candidate.root], candidate)
	}

	var staged []stagedRoot
	for _, managedRoot := range e.registry.Roots() {
		destination := filepath.Join(root, filepath.FromSlash(managedRoot))
		parent := filepath.Dir(destination)
		if err := ensureDirectoryBelowRoot(root, parent); err != nil {
			_ = cleanupStagedRoots(staged)
			return nil, fmt.Errorf("prepare artifact root %q: %w", managedRoot, err)
		}
		staging, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+".artifact-stage-*")
		if err != nil {
			_ = cleanupStagedRoots(staged)
			return nil, fmt.Errorf("stage artifact root %q: %w", managedRoot, err)
		}
		item := stagedRoot{destination: destination, staging: staging}
		staged = append(staged, item)
		if !e.exact {
			info, err := os.Lstat(destination)
			if err == nil {
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf("managed artifact root %q is not a directory", managedRoot)
				}
				if err := copyTree(os.DirFS(destination), staging); err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf("preserve managed artifact root %q: %w", managedRoot, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				_ = cleanupStagedRoots(staged)
				return nil, fmt.Errorf("inspect managed artifact root %q: %w", managedRoot, err)
			}
		}

		for _, candidate := range byRoot[managedRoot] {
			var sourceBefore [32]byte
			if candidate.mutable {
				sourceBefore, err = digestTree(candidate.filesystem)
				if err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf(
						"snapshot local artifact %s: %w",
						candidate.result.ResourceID.String(), err,
					)
				}
			}
			relative, err := filepath.Rel(destination, candidate.result.Destination)
			if err != nil || relative == ".." ||
				strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				_ = cleanupStagedRoots(staged)
				return nil, fmt.Errorf(
					"resolve artifact %s below managed root %q",
					candidate.result.ResourceID.String(), managedRoot,
				)
			}
			target := filepath.Join(staging, relative)
			if relative == "." {
				if err := clearDirectory(target); err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf("clear staged artifact root: %w", err)
				}
			} else {
				if err := os.RemoveAll(target); err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf("replace staged artifact destination: %w", err)
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf("create staged artifact parent: %w", err)
				}
				if err := os.Mkdir(target, 0o755); err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf("create staged artifact destination: %w", err)
				}
			}
			if err := copyTree(candidate.filesystem, target); err != nil {
				_ = cleanupStagedRoots(staged)
				return nil, fmt.Errorf(
					"stage artifact %s: %w",
					candidate.result.ResourceID.String(), err,
				)
			}
			if candidate.mutable {
				stagedDigest, stagedErr := digestTree(os.DirFS(target))
				sourceAfter, sourceErr := digestTree(candidate.filesystem)
				if err := errors.Join(stagedErr, sourceErr); err != nil {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf(
						"verify local artifact snapshot %s: %w",
						candidate.result.ResourceID.String(), err,
					)
				}
				if sourceBefore != stagedDigest || sourceBefore != sourceAfter {
					_ = cleanupStagedRoots(staged)
					return nil, fmt.Errorf(
						"local artifact %s changed while it was being materialized",
						candidate.result.ResourceID.String(),
					)
				}
			}
		}
	}
	return staged, nil
}

func clearDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		errs = append(errs, os.RemoveAll(filepath.Join(directory, entry.Name())))
	}
	return errors.Join(errs...)
}

func pathClean(value string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
}

func activateRoot(staged stagedRoot) (activatedRoot, error) {
	activated := activatedRoot{
		destination: staged.destination,
		staging:     staged.staging,
	}
	destination := staged.destination
	parent := filepath.Dir(destination)
	if _, err := os.Lstat(destination); err == nil {
		backup, reserveErr := reserveSiblingPath(parent, "."+filepath.Base(destination)+".artifact-backup-*")
		if reserveErr != nil {
			return activated, reserveErr
		}
		if err := os.Rename(destination, backup); err != nil {
			return activated, fmt.Errorf("move existing artifact to backup: %w", err)
		}
		activated.backup = backup
		activated.hadTarget = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return activated, fmt.Errorf("inspect artifact destination: %w", err)
	}
	if err := os.Rename(staged.staging, destination); err != nil {
		if activated.hadTarget {
			restoreErr := os.Rename(activated.backup, destination)
			if restoreErr == nil {
				activated.backup = ""
				activated.hadTarget = false
			}
			return activated, errors.Join(err, restoreErr)
		}
		return activated, fmt.Errorf("activate staged artifact root: %w", err)
	}
	activated.staging = ""
	return activated, nil
}

func rollbackActivated(activated []activatedRoot) ([]activatedRoot, error) {
	var errs []error
	var remaining []activatedRoot
	for i := len(activated) - 1; i >= 0; i-- {
		item := activated[i]
		destination := item.destination
		if destination != "" {
			if err := os.RemoveAll(destination); err != nil {
				errs = append(errs, fmt.Errorf("remove materialized artifact root %s: %w", destination, err))
				remaining = append(remaining, item)
				continue
			}
			item.destination = ""
		}
		if item.hadTarget {
			if err := os.Rename(item.backup, destination); err != nil {
				item.destination = destination
				errs = append(errs, fmt.Errorf("restore artifact root %s: %w", destination, err))
				remaining = append(remaining, item)
				continue
			}
			item.hadTarget = false
			item.backup = ""
		}
		if item.staging != "" {
			if err := os.RemoveAll(item.staging); err != nil {
				errs = append(errs, fmt.Errorf("remove staged artifact root %s: %w", item.staging, err))
				remaining = append(remaining, item)
			}
		}
	}
	return remaining, errors.Join(errs...)
}

func cleanupStagedRoots(staged []stagedRoot) error {
	var errs []error
	for _, item := range staged {
		if item.staging == "" {
			continue
		}
		if err := os.RemoveAll(item.staging); err != nil {
			errs = append(errs, fmt.Errorf("remove staged artifact root %s: %w", item.staging, err))
		}
	}
	return errors.Join(errs...)
}

func reserveSiblingPath(parent, pattern string) (string, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve artifact backup: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("prepare artifact backup: %w", err)
	}
	return path, nil
}
