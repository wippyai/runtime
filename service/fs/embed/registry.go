// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"

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

// entryTarget is the pack filesystem currently backing one served entry,
// together with the version of the pack it came from.
type entryTarget struct {
	fsys    fs.ReadDirFS
	version string
}

// entryFS serves one module-owned entry from the pack that currently backs it.
// The target is swapped by RetargetModule, so a filesystem a consumer cached
// keeps reading after an identical-content version bump retires the pack the
// entry was resolved from. Reads cost one atomic load and then go straight to
// the pack filesystem — no per-read registry lookup.
type entryFS struct {
	target atomic.Pointer[entryTarget]
	id     registry.ID
}

func newEntryFS(id registry.ID, target *entryTarget) *entryFS {
	f := &entryFS{id: id}
	f.target.Store(target)
	return f
}

func (f *entryFS) Open(name string) (fs.File, error) {
	return f.target.Load().fsys.Open(name)
}

func (f *entryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return f.target.Load().fsys.ReadDir(name)
}

// version reports the pack version currently serving this entry.
func (f *entryFS) version() string {
	return f.target.Load().version
}

// Registry implements embedapi.Registry by storing per-pack readers.
//
// Packs are keyed by their pack path (the cached .wapp location at runtime, or
// a synthetic key at boot). Each pack records its owning module and version so
// a specific (module, version) pack can be located and replaced independently —
// this lets module updates stage a new pack while the old one keeps serving
// until commit, and lets uninstall close exactly the pack it removed.
//
// Filesystems handed out for module-owned entries are tracked per module so
// RetargetModule can repoint them when a version bump produces no entry event.
type Registry struct {
	packs map[string]*pack
	// served indexes the live entry filesystems by owning module.
	served map[string]map[*entryFS]struct{}
	mu     sync.RWMutex
}

// NewRegistry creates a new embed registry.
func NewRegistry() *Registry {
	return &Registry{
		packs:  make(map[string]*pack),
		served: make(map[string]map[*entryFS]struct{}),
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
	r.dropServedLocked(p.module, p.version)
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
	r.dropServedLocked(module, version)
	return firstErr
}

// HasModulePack reports whether a pack for the exact module and version is
// currently registered.
func (r *Registry) HasModulePack(module, version string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.hasModulePackLocked(module, version)
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
// owns the module and version the operation's provenance names. This is
// deterministic across updates: while a new pack is staged alongside the old
// one, the operation's own provenance selects the correct version. Entries with
// no provenance, or host-authored provenance, fall back to GetFS. Versioned
// module entries require their exact pack to be registered so updates never
// silently serve an older pack.
func (r *Registry) GetFSForEntry(entry registry.Entry, prov *registry.EntryProvenance) (fs.ReadDirFS, error) {
	if prov != nil && prov.Module != "" {
		fsys, found, err := r.getFSForModuleEntry(entry.ID, prov.Module, prov.Version)
		if found {
			return fsys, err
		}
		if prov.Version != "" {
			return nil, systemfs.NewFilesystemNotFoundWithCauseError(entry.ID.String(), fs.ErrNotExist)
		}
	}

	return r.GetFS(entry.ID)
}

// getFSForModuleEntry resolves an entry against the module's packs and tracks
// the filesystem it hands out so RetargetModule can repoint it later. Resolving
// an entry already served from the same pack version returns the filesystem
// already tracked for it; tracking is dropped when the pack version retires.
func (r *Registry) getFSForModuleEntry(id registry.ID, module, version string) (fs.ReadDirFS, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	target, found, err := r.resolveTargetLocked(id, module, version)
	if !found || err != nil {
		return nil, found, err
	}

	for served := range r.served[module] {
		if served.id == id && served.version() == target.version {
			return served, true, nil
		}
	}

	served := newEntryFS(id, target)
	if r.served[module] == nil {
		r.served[module] = make(map[*entryFS]struct{})
	}
	r.served[module][served] = struct{}{}

	return served, true, nil
}

// resolveTargetLocked finds the pack filesystem for an entry. An empty version
// accepts any pack of the module.
func (r *Registry) resolveTargetLocked(id registry.ID, module, version string) (*entryTarget, bool, error) {
	for _, p := range r.packs {
		if p.module != module {
			continue
		}
		if version != "" && p.version != version {
			continue
		}
		if !p.owns(id) {
			continue
		}
		fsys, err := p.reader.GetFS(wapp.NewID(id.NS, id.Name))
		if err != nil {
			return nil, true, err
		}
		return &entryTarget{fsys: fsys, version: p.version}, true, nil
	}

	return nil, false, nil
}

// RetargetModule repoints every filesystem served from the module's fromVersion
// pack at its toVersion pack. It is the path for an identical-content version
// bump: entries whose content did not change receive no event, so nothing
// re-resolves them, yet Finalize retires the old pack. Calling this before the
// old pack closes keeps those cached filesystems reading.
//
// Resolution happens for every affected entry before any swap, so a module
// whose new pack is missing an entry retargets nothing and reports the failure.
// Having nothing to retarget — no filesystem served from fromVersion — is a
// no-op, including for modules that never had a pack.
func (r *Registry) RetargetModule(module, fromVersion, toVersion string) error {
	if module == "" {
		return systemfs.NewEmptyModuleError()
	}
	if fromVersion == toVersion {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var stale []*entryFS
	for served := range r.served[module] {
		if served.version() == fromVersion {
			stale = append(stale, served)
		}
	}
	if len(stale) == 0 {
		return nil
	}

	if !r.hasModulePackLocked(module, toVersion) {
		return systemfs.NewModulePackNotFoundError(module, toVersion)
	}

	targets := make([]*entryTarget, len(stale))
	for i, served := range stale {
		target, found, err := r.resolveTargetLocked(served.id, module, toVersion)
		if err != nil {
			return err
		}
		if !found {
			return systemfs.NewFilesystemNotFoundWithCauseError(served.id.String(), fs.ErrNotExist)
		}
		targets[i] = target
	}

	for i, served := range stale {
		served.target.Store(targets[i])
	}
	return nil
}

func (r *Registry) hasModulePackLocked(module, version string) bool {
	for _, p := range r.packs {
		if p.module == module && p.version == version {
			return true
		}
	}
	return false
}

// dropServedLocked forgets the filesystems served from a module version whose
// last pack is gone. Their pack is closed, so they can no longer serve reads.
func (r *Registry) dropServedLocked(module, version string) {
	if module == "" || r.hasModulePackLocked(module, version) {
		return
	}
	for served := range r.served[module] {
		if served.version() == version {
			delete(r.served[module], served)
		}
	}
	if len(r.served[module]) == 0 {
		delete(r.served, module)
	}
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
	r.served = make(map[string]map[*entryFS]struct{})

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
