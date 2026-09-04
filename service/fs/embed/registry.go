// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	systemfs "github.com/wippyai/runtime/system/fs"
	"github.com/wippyai/wapp"
)

// pack is one opened WAPP. The registry retains it while it is active; reads
// retain it independently, so retiring a pack never closes a file in use.
type pack struct {
	reader    *wapp.Reader
	file      *os.File
	resources map[registry.ID]struct{}
	path      string
	module    string
	version   string
	active    atomic.Bool
	refs      atomic.Int64
}

func (p *pack) owns(id registry.ID) bool {
	_, ok := p.resources[id]
	return ok
}

func (p *pack) retain() bool {
	for {
		refs := p.refs.Load()
		if refs <= 0 {
			return false
		}
		if p.refs.CompareAndSwap(refs, refs+1) {
			return true
		}
	}
}

func (p *pack) release() error {
	if p.refs.Add(-1) != 0 {
		return nil
	}
	return closeFile(p.file)
}

// Registry owns opened WAPPs and the active resource mapping. Resource IDs
// are the sole filesystem identity. Activating a staged pack switches each ID
// it carries; removing it restores the preceding active pack, if any.
//
// Module and version identify a pack only for dependency lifecycle cleanup.
// They are not consulted when resolving a filesystem.
type Registry struct {
	packs     map[string]*pack
	resources map[registry.ID][]*pack
	staged    map[registry.ID][]*pack
	mu        sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		packs:     make(map[string]*pack),
		resources: make(map[registry.ID][]*pack),
		staged:    make(map[registry.ID][]*pack),
	}
}

// Register adds a pack that is not managed by a dependency operation.
func (r *Registry) Register(path string, reader *wapp.Reader, file *os.File) error {
	return r.RegisterPack(path, "", "", reader, file)
}

// RegisterPack opens and activates a pack. Boot uses this after the selected
// pack set has been resolved.
func (r *Registry) RegisterPack(path, module, version string, reader *wapp.Reader, file *os.File) error {
	return r.registerPack(path, module, version, reader, file, true)
}

// StagePack opens a pack without changing any active resource. Dependency
// effects call it during Prepare, before the registry transition is committed.
func (r *Registry) StagePack(path, module, version string, reader *wapp.Reader, file *os.File) error {
	return r.registerPack(path, module, version, reader, file, false)
}

func (r *Registry) registerPack(path, module, version string, reader *wapp.Reader, file *os.File, activate bool) error {
	if path == "" {
		return systemfs.NewEmptyPackPathError()
	}
	if reader == nil {
		return systemfs.NewNilReaderError()
	}

	resources := make(map[registry.ID]struct{})
	for _, resource := range reader.ListResources() {
		resources[registry.NewID(resource.ID.Namespace, resource.ID.Name)] = struct{}{}
	}
	next := &pack{
		reader:    reader,
		file:      file,
		resources: resources,
		path:      path,
		module:    module,
		version:   version,
	}
	next.refs.Store(1)
	next.active.Store(activate)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.packs[path] != nil {
		return systemfs.NewFilesystemAlreadyExistsError(path)
	}
	if err := r.validateResourcesLocked(path, module, resources); err != nil {
		return err
	}
	r.packs[path] = next
	if activate {
		for id := range resources {
			r.resources[id] = append(r.resources[id], next)
		}
	} else {
		for id := range resources {
			r.staged[id] = append(r.staged[id], next)
		}
	}
	return nil
}

// ActivatePack makes a staged pack the active source for each resource it
// carries. It is called by Effect.Commit after registry listeners accepted the
// corresponding transition.
func (r *Registry) ActivatePack(path string) error {
	return r.ActivatePacks([]string{path})
}

// ActivatePacks switches a prepared pack set under one lock. Effects use this
// to avoid exposing a partial module transition.
func (r *Registry) ActivatePacks(paths []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	packs := make([]*pack, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		current := r.packs[path]
		if current == nil {
			return systemfs.NewFilesystemNotFoundError(path)
		}
		if current.active.Load() {
			continue
		}
		if err := r.validateResourcesLocked(path, current.module, current.resources); err != nil {
			return err
		}
		packs = append(packs, current)
	}
	for _, current := range packs {
		for id := range current.resources {
			r.removeStagedLocked(id, current)
			r.resources[id] = append(r.resources[id], current)
		}
		current.active.Store(true)
	}
	return nil
}

func (r *Registry) validateResourcesLocked(path, module string, resources map[registry.ID]struct{}) error {
	for id := range resources {
		if staged := r.stagedPackLocked(id); staged != nil && staged.path != path {
			return systemfs.NewEmbeddedResourceConflictError(id.String(), staged.path, path)
		}
		active := r.activePackLocked(id)
		if active == nil || active.path == path || (module != "" && active.module == module) {
			continue
		}
		return systemfs.NewEmbeddedResourceConflictError(id.String(), active.path, path)
	}
	return nil
}

// UnregisterPack retires a pack. Any resource it was actively serving moves
// back to the preceding active pack; unrelated resource mappings are intact.
func (r *Registry) UnregisterPack(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.packs[path]; current != nil {
		return r.removePackLocked(current)
	}
	return nil
}

// UnregisterModule retires the exact module version after a successful
// dependency transition. It does not participate in resource selection.
func (r *Registry) UnregisterModule(module, version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, current := range r.packs {
		if current.module != module || current.version != version {
			continue
		}
		if err := r.removePackLocked(current); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Registry) HasModulePack(module, version string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, current := range r.packs {
		if current.active.Load() && current.module == module && current.version == version {
			return true
		}
	}
	return false
}

// GetFS returns a stable resource handle. A handle issued while a pack is
// staged reads that pack for the enclosing transition; existing handles remain
// on the active pack until Commit. After Commit, both use the active mapping.
func (r *Registry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	r.mu.RLock()
	staged := r.stagedPackLocked(id)
	found := staged != nil || r.activePackLocked(id) != nil
	r.mu.RUnlock()
	if !found {
		return nil, systemfs.NewFilesystemNotFoundWithCauseError(id.String(), fs.ErrNotExist)
	}
	return resourceFS{registry: r, id: id, staged: staged}, nil
}

func (r *Registry) activePackLocked(id registry.ID) *pack {
	packs := r.resources[id]
	if len(packs) == 0 {
		return nil
	}
	return packs[len(packs)-1]
}

func (r *Registry) stagedPackLocked(id registry.ID) *pack {
	packs := r.staged[id]
	if len(packs) == 0 {
		return nil
	}
	return packs[len(packs)-1]
}

func (r *Registry) acquire(id registry.ID) (*pack, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	current := r.activePackLocked(id)
	if current == nil {
		return nil, false
	}
	return current, current.retain()
}

func (r *Registry) removePackLocked(current *pack) error {
	if r.packs[current.path] != current {
		return nil
	}
	delete(r.packs, current.path)
	if current.active.Load() {
		for id := range current.resources {
			packs := r.resources[id]
			for i, candidate := range packs {
				if candidate != current {
					continue
				}
				packs = append(packs[:i], packs[i+1:]...)
				break
			}
			if len(packs) == 0 {
				delete(r.resources, id)
			} else {
				r.resources[id] = packs
			}
		}
	} else {
		for id := range current.resources {
			r.removeStagedLocked(id, current)
		}
	}
	current.active.Store(false)
	return current.release()
}

func (r *Registry) removeStagedLocked(id registry.ID, current *pack) {
	packs := r.staged[id]
	for i, candidate := range packs {
		if candidate != current {
			continue
		}
		packs = append(packs[:i], packs[i+1:]...)
		break
	}
	if len(packs) == 0 {
		delete(r.staged, id)
		return
	}
	r.staged[id] = packs
}

func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, current := range r.packs {
		if err := r.removePackLocked(current); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type resourceFS struct {
	registry *Registry
	staged   *pack
	id       registry.ID
}

func (f resourceFS) Open(name string) (fs.File, error) {
	current, ok := f.acquire()
	if !ok {
		return nil, f.notFound()
	}

	fsys, err := current.reader.GetFS(wapp.NewID(f.id.NS, f.id.Name))
	if err != nil {
		_ = current.release()
		return nil, err
	}
	file, err := fsys.Open(name)
	if err != nil {
		_ = current.release()
		return nil, err
	}
	return &retainedFile{File: file, pack: current}, nil
}

func (f resourceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	current, ok := f.acquire()
	if !ok {
		return nil, f.notFound()
	}
	defer func() { _ = current.release() }()

	fsys, err := current.reader.GetFS(wapp.NewID(f.id.NS, f.id.Name))
	if err != nil {
		return nil, err
	}
	return fsys.ReadDir(name)
}

func (f resourceFS) acquire() (*pack, bool) {
	if f.staged != nil && !f.staged.active.Load() && f.staged.retain() {
		return f.staged, true
	}
	return f.registry.acquire(f.id)
}

func (f resourceFS) notFound() error {
	return systemfs.NewFilesystemNotFoundWithCauseError(f.id.String(), fs.ErrNotExist)
}

type retainedFile struct {
	fs.File
	pack     *pack
	released atomic.Bool
}

func (f *retainedFile) Close() error {
	err := f.File.Close()
	if f.released.CompareAndSwap(false, true) {
		_ = f.pack.release()
	}
	return err
}

func (f *retainedFile) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := f.File.(io.Seeker)
	if !ok {
		return 0, &fs.PathError{Op: "seek", Err: fs.ErrInvalid}
	}
	return seeker.Seek(offset, whence)
}

func (f *retainedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	dir, ok := f.File.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Err: fs.ErrInvalid}
	}
	return dir.ReadDir(n)
}

func closeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}

// GetRegistryFromContext retrieves the concrete Registry from context.
func GetRegistryFromContext(ctx context.Context) *Registry {
	reg := embedapi.GetRegistry(ctx)
	if reg == nil {
		return nil
	}
	if current, ok := reg.(*Registry); ok {
		return current
	}
	return nil
}
