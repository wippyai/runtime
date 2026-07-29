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

// Materialized describes one artifact output owned by a WAPP resource.
type Materialized struct {
	Descriptor  Descriptor
	Destination string
	ResourceID  wapp.ID
	WAPPPath    string
}

type wappEffectState uint8

const (
	wappEffectPlanned wappEffectState = iota
	wappEffectPrepared
	wappEffectCommitted
	wappEffectRolledBack
	wappEffectFinalized
)

type activatedArtifact struct {
	result    Materialized
	backup    string
	hadTarget bool
}

// WAPPEffect materializes artifact resources as a registry-compatible
// transaction effect. It deliberately does not resolve or download modules.
type WAPPEffect struct {
	registry  *Registry
	packs     []WAPP
	root      string
	activated []activatedArtifact
	results   []Materialized
	state     wappEffectState
	mu        sync.Mutex
}

// NewWAPPEffect creates a materialization effect for exact, verified WAPPs.
func NewWAPPEffect(registry *Registry, packs []WAPP, root string) (*WAPPEffect, error) {
	if registry == nil {
		return nil, errors.New("artifact registry is nil")
	}
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact root is empty")
	}
	return &WAPPEffect{
		registry: registry,
		packs:    append([]WAPP(nil), packs...),
		root:     root,
	}, nil
}

// Results returns a copy of the successfully prepared outputs.
func (e *WAPPEffect) Results() []Materialized {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Materialized(nil), e.results...)
}

// Prepare validates every declared artifact before activating any output, then
// retains prior outputs for rollback.
func (e *WAPPEffect) Prepare(ctx context.Context) error {
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
	defer closePacks()

	for _, candidate := range candidates {
		activated, activateErr := activateArtifact(candidate)
		if activateErr != nil {
			rollbackErr := rollbackActivated(e.activated)
			if rollbackErr == nil {
				e.activated = nil
				e.results = nil
				e.state = wappEffectRolledBack
			}
			return errors.Join(activateErr, rollbackErr)
		}
		e.activated = append(e.activated, activated)
		e.results = append(e.results, activated.result)
	}
	e.state = wappEffectPrepared
	return nil
}

// Commit marks the prepared outputs as committed while keeping rollback data
// until the surrounding transaction is durable.
func (e *WAPPEffect) Commit(context.Context) error {
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
func (e *WAPPEffect) Rollback(context.Context) error {
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
	if err := rollbackActivated(e.activated); err != nil {
		return err
	}
	e.activated = nil
	e.results = nil
	e.state = wappEffectRolledBack
	return nil
}

// Finalize removes rollback data after the surrounding transaction is durable.
func (e *WAPPEffect) Finalize(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == wappEffectFinalized {
		return nil
	}
	if e.state != wappEffectPrepared && e.state != wappEffectCommitted {
		return fmt.Errorf("finalize artifact effect in state %d", e.state)
	}
	var errs []error
	for _, activated := range e.activated {
		if activated.backup == "" {
			continue
		}
		if err := os.RemoveAll(activated.backup); err != nil {
			errs = append(errs, fmt.Errorf("remove artifact backup %s: %w", activated.result.Destination, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	e.activated = nil
	e.state = wappEffectFinalized
	return nil
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
	result     Materialized
}

func (e *WAPPEffect) inspect(ctx context.Context) ([]artifactCandidate, func(), error) {
	root, err := filepath.Abs(e.root)
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve artifact root: %w", err)
	}

	var files []*os.File
	closePacks := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	var candidates []artifactCandidate
	destinations := make(map[string]wapp.ID)
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
			declaration, declared, parseErr := ParseDeclaration(resource.Meta)
			if parseErr != nil {
				closePacks()
				return nil, func() {}, fmt.Errorf("resource %s in %s: %w", resource.ID.String(), pack.Path, parseErr)
			}
			if !declared {
				continue
			}
			filesystem, fsErr := reader.GetFS(resource.ID)
			if fsErr != nil {
				closePacks()
				return nil, func() {}, fmt.Errorf("open resource %s in %s: %w", resource.ID.String(), pack.Path, fsErr)
			}
			descriptor, inspectErr := e.registry.Inspect(ctx, declaration, InspectInput{
				Filesystem:    filesystem,
				ModuleVersion: moduleVersion,
				ResourceID:    resource.ID,
			})
			if inspectErr != nil {
				closePacks()
				return nil, func() {}, inspectErr
			}
			destination := filepath.Join(root, filepath.FromSlash(descriptor.RelativePath))
			if withinErr := ensureWithinRoot(root, destination); withinErr != nil {
				closePacks()
				return nil, func() {}, withinErr
			}
			key := strings.ToLower(filepath.Clean(destination))
			if previous, exists := destinations[key]; exists {
				closePacks()
				return nil, func() {}, fmt.Errorf(
					"artifact resources %s and %s materialize to the same path %q",
					previous.String(), resource.ID.String(), descriptor.RelativePath)
			}
			destinations[key] = resource.ID
			candidates = append(candidates, artifactCandidate{
				filesystem: filesystem,
				result: Materialized{
					Descriptor:  descriptor,
					Destination: destination,
					ResourceID:  resource.ID,
					WAPPPath:    pack.Path,
				},
			})
		}
	}
	return candidates, closePacks, nil
}

func activateArtifact(candidate artifactCandidate) (activatedArtifact, error) {
	activated := activatedArtifact{result: candidate.result}
	destination := candidate.result.Destination
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return activated, fmt.Errorf("create artifact parent: %w", err)
	}
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
	if err := exactMirror(candidate.filesystem, destination); err != nil {
		if activated.hadTarget {
			restoreErr := os.Rename(activated.backup, destination)
			if restoreErr == nil {
				activated.backup = ""
			}
			return activated, errors.Join(err, restoreErr)
		}
		return activated, err
	}
	return activated, nil
}

func rollbackActivated(activated []activatedArtifact) error {
	var errs []error
	for i := len(activated) - 1; i >= 0; i-- {
		item := activated[i]
		if err := os.RemoveAll(item.result.Destination); err != nil {
			errs = append(errs, fmt.Errorf("remove materialized artifact %s: %w", item.result.Destination, err))
			continue
		}
		if item.hadTarget {
			if err := os.Rename(item.backup, item.result.Destination); err != nil {
				errs = append(errs, fmt.Errorf("restore artifact %s: %w", item.result.Destination, err))
			}
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
