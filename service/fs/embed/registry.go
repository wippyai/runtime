// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	systemfs "github.com/wippyai/runtime/system/fs"
	"github.com/wippyai/wapp"
)

// pack holds a single registered .wapp reader together with the resources it
// owns and the optional file handle that backs it. The handle is owned by the
// registry and closed when the pack is unregistered or the registry is closed.
type pack struct {
	reader    *wapp.Reader
	file      *os.File
	resources map[registry.ID]struct{}
	packPath  string
	module    string
	version   string
}

// owns reports whether the pack exposes the given resource ID.
func (p *pack) owns(id registry.ID) bool {
	_, ok := p.resources[id]
	return ok
}

// Registry implements embedapi.Registry by storing per-pack readers.
//
// Packs are keyed by their pack path (the cached .wapp location at runtime, or
// a synthetic key at boot). Each pack records its owning module and version so
// a specific (module, version) pack can be located and replaced independently —
// this lets module updates stage a new pack while the old one keeps serving
// until commit, and lets uninstall close exactly the pack it removed.
type Registry struct {
	packs map[string]*pack
	mu    sync.RWMutex
}

// NewRegistry creates a new embed registry.
func NewRegistry() *Registry {
	return &Registry{
		packs: make(map[string]*pack),
	}
}

// Register adds a pack reader to the registry without module ownership.
// Retained for callers that have no module identity. The pack path is used as
// the lookup key; if a file handle is provided it is owned by the registry and
// closed on unregister/close.
func (r *Registry) Register(packPath string, reader *wapp.Reader, file *os.File) error {
	return r.RegisterPack(packPath, "", "", reader, file)
}

// RegisterPack adds a pack reader tagged with its owning module and version.
// Registering a pack path that already exists replaces the previous pack and
// closes its file handle, so re-registration never leaks the prior handle.
func (r *Registry) RegisterPack(packPath, module, version string, reader *wapp.Reader, file *os.File) error {
	if packPath == "" {
		return systemfs.NewEmptyPackPathError()
	}
	if reader == nil {
		return systemfs.NewNilReaderError()
	}

	resources := make(map[registry.ID]struct{})
	for _, res := range reader.ListResources() {
		resources[registry.NewID(res.ID.Namespace, res.ID.Name)] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.packs[packPath]; ok {
		if err := closeFile(existing.file); err != nil {
			return err
		}
	}

	r.packs[packPath] = &pack{
		packPath:  packPath,
		module:    module,
		version:   version,
		reader:    reader,
		file:      file,
		resources: resources,
	}
	return nil
}

// UnregisterPack removes the pack registered under packPath and closes its file
// handle. Unregistering an unknown pack path is a no-op.
func (r *Registry) UnregisterPack(packPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.packs[packPath]
	if !ok {
		return nil
	}
	delete(r.packs, packPath)
	return closeFile(p.file)
}

// UnregisterModule removes every pack owned by the given module and version,
// closing their file handles. Unregistering a module that is not present is a
// no-op. Returns the first close error encountered, if any.
func (r *Registry) UnregisterModule(module, version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for key, p := range r.packs {
		if p.module != module || p.version != version {
			continue
		}
		delete(r.packs, key)
		if err := closeFile(p.file); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GetFS implements embedapi.Registry.GetFS.
// It searches all registered packs for a resource with the given ID and returns
// the first match. Use GetFSForEntry when a specific module version must be
// selected (e.g. during an update where two versions expose the same ID).
func (r *Registry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	wappID := wapp.NewID(id.NS, id.Name)
	for _, p := range r.packs {
		if !p.owns(id) {
			continue
		}
		fsys, err := p.reader.GetFS(wappID)
		if err == nil {
			return fsys, nil
		}
	}

	return nil, systemfs.NewFilesystemNotFoundWithCauseError(id.String(), fs.ErrNotExist)
}

// GetFSForEntry resolves the filesystem for an entry, preferring the pack that
// owns the entry's module and version (from meta.module / meta.module_version).
// This is deterministic across updates: while a new pack is staged alongside the
// old one, the entry's own metadata selects the correct version. Entries without
// module metadata fall back to GetFS. Versioned module entries require their
// exact pack to be registered so updates never silently serve an older pack.
func (r *Registry) GetFSForEntry(entry registry.Entry) (fs.ReadDirFS, error) {
	module := entryMeta(entry, metaModuleKey)
	version := entryMeta(entry, metaModuleVersionKey)

	if module != "" {
		fsys, found, err := r.getFSForModuleEntry(entry, module, version)
		if found {
			return fsys, err
		}
		if version != "" {
			return nil, systemfs.NewFilesystemNotFoundWithCauseError(entry.ID.String(), fs.ErrNotExist)
		}
	}

	return r.GetFS(entry.ID)
}

func (r *Registry) getFSForModuleEntry(entry registry.Entry, module, version string) (fs.ReadDirFS, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.packs {
		if p.module != module {
			continue
		}
		if version != "" && p.version != version {
			continue
		}
		if !p.owns(entry.ID) {
			continue
		}
		fsys, err := p.reader.GetFS(wapp.NewID(entry.ID.NS, entry.ID.Name))
		return fsys, true, err
	}

	return nil, false, nil
}

// Close implements embedapi.Registry.Close.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, p := range r.packs {
		if err := closeFile(p.file); err != nil {
			errs = append(errs, err)
		}
	}
	r.packs = make(map[string]*pack)

	if len(errs) > 0 {
		return fmt.Errorf("failed to close %d file(s)", len(errs))
	}
	return nil
}

func closeFile(f *os.File) error {
	if f == nil {
		return nil
	}
	return f.Close()
}

const (
	metaModuleKey        = "module"
	metaModuleVersionKey = "module_version"
)

func entryMeta(entry registry.Entry, key string) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetRegistryFromContext retrieves the concrete Registry from context.
// Returns nil if not found or if the registry is a different implementation.
func GetRegistryFromContext(ctx context.Context) *Registry {
	reg := embedapi.GetRegistry(ctx)
	if reg == nil {
		return nil
	}
	if r, ok := reg.(*Registry); ok {
		return r
	}
	return nil
}
