// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"fmt"
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

// pack holds a single registered .wapp reader together with the resources it
// owns and the optional file handle that backs it. The handle is owned by the
// registry and reference counted: registration holds one reference, every
// generation serving the pack holds one, and every read in flight holds one.
// The file closes when the last reference goes away, so a read can never touch
// a closed handle.
type pack struct {
	reader    *wapp.Reader
	file      *os.File
	resources map[registry.ID]struct{}
	packPath  string
	module    string
	version   string
	refs      atomic.Int64
}

// owns reports whether the pack exposes the given resource ID.
func (p *pack) owns(id registry.ID) bool {
	_, ok := p.resources[id]
	return ok
}

// retain takes a reference. It fails once the pack is closed, which is what
// makes a read against a retired generation fall through to the current one
// instead of reading a closed file.
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

// release drops a reference and closes the file handle on the last one. The
// close is never waited on by another goroutine, so releasing cannot deadlock;
// a close deferred behind reads in flight reports its error to nobody.
func (p *pack) release() error {
	if p.refs.Add(-1) != 0 {
		return nil
	}
	return closeFile(p.file)
}

// generation binds one pack to the entries served from it. The file map is
// immutable once published, so readers hold no lock; serving a new entry
// publishes a copy that carries the additional entry.
type generation struct {
	pack  *pack
	files map[registry.ID]fs.ReadDirFS
}

// version reports the pack version this generation serves.
func (g *generation) version() string {
	return g.pack.version
}

// with returns a generation of the same pack serving one more entry.
func (g *generation) with(id registry.ID, fsys fs.ReadDirFS) *generation {
	files := make(map[registry.ID]fs.ReadDirFS, len(g.files)+1)
	for served, servedFS := range g.files {
		files[served] = servedFS
	}
	files[id] = fsys
	return &generation{pack: g.pack, files: files}
}

// lineage is the generation pointer shared by every filesystem the registry
// served from one module version. Retargeting is a single store on it: all of
// that version's filesystems change pack together, with no per-filesystem
// bookkeeping and no registry-owned reference to any of them.
type lineage struct {
	current atomic.Pointer[generation]
}

func newLineage(gen *generation) *lineage {
	l := &lineage{}
	l.current.Store(gen)
	return l
}

// acquire returns the current generation holding a reference on its pack. The
// caller must release it when its read finishes. A generation whose pack has
// already closed is skipped in favor of the generation that replaced it.
func (l *lineage) acquire() (*generation, bool) {
	for {
		gen := l.current.Load()
		if gen == nil {
			return nil, false
		}
		if gen.pack.retain() {
			return gen, true
		}
		if l.current.Load() == gen {
			return nil, false
		}
	}
}

// entryFS is the filesystem handed to a consumer for one entry. It resolves
// through its lineage, so a retarget reaches it without the registry holding a
// reference to it. A read costs one atomic load and one map lookup.
type entryFS struct {
	lineage *lineage
	id      registry.ID
}

func (f *entryFS) Open(name string) (fs.File, error) {
	gen, ok := f.lineage.acquire()
	if !ok {
		return nil, f.notFound()
	}

	fsys, ok := gen.files[f.id]
	if !ok {
		_ = gen.pack.release()
		return nil, f.notFound()
	}

	file, err := fsys.Open(name)
	if err != nil {
		_ = gen.pack.release()
		return nil, err
	}

	// Pack data is read lazily, so the reference travels with the handle.
	return &refFile{file: file, pack: gen.pack, name: name}, nil
}

func (f *entryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	gen, ok := f.lineage.acquire()
	if !ok {
		return nil, f.notFound()
	}
	defer func() { _ = gen.pack.release() }()

	fsys, ok := gen.files[f.id]
	if !ok {
		return nil, f.notFound()
	}
	return fsys.ReadDir(name)
}

func (f *entryFS) notFound() error {
	return systemfs.NewFilesystemNotFoundWithCauseError(f.id.String(), fs.ErrNotExist)
}

// refFile holds its pack reference until the file is closed. A consumer that
// never closes a file keeps that pack's handle open, the same contract an
// *os.File carries.
type refFile struct {
	file     fs.File
	pack     *pack
	name     string
	released atomic.Bool
}

func (f *refFile) Read(p []byte) (int, error) {
	return f.file.Read(p)
}

func (f *refFile) Stat() (fs.FileInfo, error) {
	return f.file.Stat()
}

func (f *refFile) Close() error {
	err := f.file.Close()
	if f.released.CompareAndSwap(false, true) {
		_ = f.pack.release()
	}
	return err
}

// Seek forwards to a seekable pack file and reports the same unsupported-file
// error the standard library's fs adapters use otherwise.
func (f *refFile) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := f.file.(io.Seeker)
	if !ok {
		return 0, &fs.PathError{Op: "seek", Path: f.name, Err: fs.ErrInvalid}
	}
	return seeker.Seek(offset, whence)
}

// ReadDir forwards to a pack directory handle.
func (f *refFile) ReadDir(n int) ([]fs.DirEntry, error) {
	dir, ok := f.file.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: f.name, Err: fs.ErrInvalid}
	}
	return dir.ReadDir(n)
}

// Registry implements embedapi.Registry by storing per-pack readers.
//
// Packs are keyed by their pack path (the cached .wapp location at runtime, or
// a synthetic key at boot). Each pack records its owning module and version so
// a specific (module, version) pack can be located and replaced independently —
// this lets module updates stage a new pack while the old one keeps serving
// until commit, and lets uninstall close exactly the pack it removed.
//
// Every filesystem the registry hands out reads through the lineage of the pack
// that served it. The registry keeps the lineages, never the filesystems, so
// retargeting reaches served filesystems without retaining them.
type Registry struct {
	packs map[string]*pack
	// lineages holds the lineages currently serving each pack.
	lineages map[*pack][]*lineage
	mu       sync.RWMutex
}

// NewRegistry creates a new embed registry.
func NewRegistry() *Registry {
	return &Registry{
		packs:    make(map[string]*pack),
		lineages: make(map[*pack][]*lineage),
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
// Registering a pack path that already exists retires the previous pack: its
// filesystems stop resolving and its handle closes once the reads in flight
// against it finish.
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

	var replaceErr error
	if existing, ok := r.packs[packPath]; ok {
		replaceErr = r.removePackLocked(existing)
	}
	// One pack per (module, version): a retarget moves ONE lineage set, so a
	// second pack at the same identity would leave readers split across packs.
	// Registering the same identity again retires the previous pack.
	if module != "" {
		for _, existing := range r.packs {
			if existing.module == module && existing.version == version {
				if err := r.removePackLocked(existing); err != nil && replaceErr == nil {
					replaceErr = err
				}
			}
		}
	}

	next := &pack{
		packPath:  packPath,
		module:    module,
		version:   version,
		reader:    reader,
		file:      file,
		resources: resources,
	}
	next.refs.Store(1)
	r.packs[packPath] = next

	return replaceErr
}

// UnregisterPack removes the pack registered under packPath and releases its
// file handle. Unregistering an unknown pack path is a no-op.
func (r *Registry) UnregisterPack(packPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.packs[packPath]
	if !ok {
		return nil
	}
	return r.removePackLocked(p)
}

// UnregisterModule removes every pack owned by the given module and version,
// releasing their file handles. Unregistering a module that is not present is a
// no-op. Returns the first close error encountered, if any.
func (r *Registry) UnregisterModule(module, version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, p := range r.packs {
		if p.module != module || p.version != version {
			continue
		}
		if err := r.removePackLocked(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// removePackLocked drops a pack from the registry: the lineages still serving
// it stop resolving, and the registration reference is released. The handle
// closes here when nothing reads it, and behind the last read otherwise.
func (r *Registry) removePackLocked(p *pack) error {
	delete(r.packs, p.packPath)

	for _, l := range r.lineages[p] {
		if gen := l.current.Swap(nil); gen != nil {
			_ = gen.pack.release()
		}
	}
	delete(r.lineages, p)

	return p.release()
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
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.packs {
		if !p.owns(id) {
			continue
		}
		fsys, err := r.serveLocked(p, id)
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

// getFSForModuleEntry resolves an entry against the module's packs. An empty
// version accepts any pack of the module.
func (r *Registry) getFSForModuleEntry(id registry.ID, module, version string) (fs.ReadDirFS, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
		fsys, err := r.serveLocked(p, id)
		return fsys, true, err
	}

	return nil, false, nil
}

// serveLocked returns the filesystem for one entry of a pack, publishing the
// entry into the pack's lineage so a later retarget carries it along.
func (r *Registry) serveLocked(p *pack, id registry.ID) (fs.ReadDirFS, error) {
	l, err := r.lineageForLocked(p)
	if err != nil {
		return nil, err
	}

	gen := l.current.Load()
	if _, ok := gen.files[id]; !ok {
		fsys, err := p.reader.GetFS(wapp.NewID(id.NS, id.Name))
		if err != nil {
			return nil, err
		}
		// The next generation serves the same pack, so the reference the
		// retired generation held carries over unchanged.
		l.current.Store(gen.with(id, fsys))
	}

	return &entryFS{lineage: l, id: id}, nil
}

// lineageForLocked returns the lineage new entries of a pack are served from,
// creating it on first use.
func (r *Registry) lineageForLocked(p *pack) (*lineage, error) {
	if existing := r.lineages[p]; len(existing) > 0 {
		return existing[0], nil
	}

	if !p.retain() {
		return nil, systemfs.NewModulePackNotFoundError(p.module, p.version)
	}
	l := newLineage(&generation{pack: p, files: make(map[registry.ID]fs.ReadDirFS)})
	r.lineages[p] = []*lineage{l}
	return l, nil
}

// RetargetModule repoints every filesystem served from the module's fromVersion
// pack at its toVersion pack. It is the path for an identical-content version
// bump: entries whose content did not change receive no event, so nothing
// re-resolves them, yet Finalize retires the old pack. Calling this before the
// old pack closes keeps those filesystems reading.
//
// Each affected lineage moves in a single store, and every entry is resolved
// against the new pack before any store happens, so a new pack missing an entry
// retargets nothing and reports the failure — the caller must then keep the old
// pack. Having nothing to retarget is a no-op, including for modules that never
// had a pack.
func (r *Registry) RetargetModule(module, fromVersion, toVersion string) error {
	if module == "" {
		return systemfs.NewEmptyModuleError()
	}
	if fromVersion == toVersion {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	stale := make(map[*pack][]*lineage)
	for p, lineages := range r.lineages {
		if p.module == module && p.version == fromVersion {
			stale[p] = lineages
		}
	}
	if len(stale) == 0 {
		return nil
	}

	target := r.packForVersionLocked(module, toVersion)
	if target == nil {
		return systemfs.NewModulePackNotFoundError(module, toVersion)
	}

	type retarget struct {
		lineage *lineage
		next    *generation
	}

	var pending []retarget
	for _, lineages := range stale {
		for _, l := range lineages {
			gen := l.current.Load()
			if gen == nil {
				continue
			}
			files := make(map[registry.ID]fs.ReadDirFS, len(gen.files))
			for id := range gen.files {
				if !target.owns(id) {
					return systemfs.NewFilesystemNotFoundWithCauseError(id.String(), fs.ErrNotExist)
				}
				fsys, err := target.reader.GetFS(wapp.NewID(id.NS, id.Name))
				if err != nil {
					return err
				}
				files[id] = fsys
			}
			pending = append(pending, retarget{lineage: l, next: &generation{pack: target, files: files}})
		}
	}

	// Every new generation holds its own reference. The target is registered,
	// so its registration reference is alive under this lock and the count can
	// be taken outright, before the first store: publishing cannot fail halfway.
	target.refs.Add(int64(len(pending)))
	for _, entry := range pending {
		if previous := entry.lineage.current.Swap(entry.next); previous != nil {
			_ = previous.pack.release()
		}
	}

	for p, lineages := range stale {
		r.lineages[target] = append(r.lineages[target], lineages...)
		delete(r.lineages, p)
	}
	return nil
}

func (r *Registry) packForVersionLocked(module, version string) *pack {
	for _, p := range r.packs {
		if p.module == module && p.version == version {
			return p
		}
	}
	return nil
}

func (r *Registry) hasModulePackLocked(module, version string) bool {
	return r.packForVersionLocked(module, version) != nil
}

// Close implements embedapi.Registry.Close.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, p := range r.packs {
		if err := r.removePackLocked(p); err != nil {
			errs = append(errs, err)
		}
	}
	r.packs = make(map[string]*pack)
	r.lineages = make(map[*pack][]*lineage)

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
