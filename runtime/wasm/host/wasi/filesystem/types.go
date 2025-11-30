package filesystem

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tetratelabs/wazero/api"

	ctxapi "github.com/wippyai/runtime/api/context"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/wasm/resource"
	streamservice "github.com/wippyai/runtime/service/dispatcher/stream"
)

const (
	// TypesNamespace is the WASI namespace for filesystem types.
	TypesNamespace = "wasi:filesystem/types@0.2.8"
)

// Resource type IDs for filesystem resources
const (
	TypeDescriptor           = resource.Handle(300)
	TypeDirectoryEntryStream = resource.Handle(301)
)

// TypesHost implements wasi:filesystem/types@0.2.8.
type TypesHost struct {
	resources   *resource.InstanceResources
	descriptors *resource.TypedTable[*Descriptor]
	dirStreams  *resource.TypedTable[*DirectoryEntryStream]
}

// NewTypesHost creates a new filesystem types host.
func NewTypesHost(resources *resource.InstanceResources) *TypesHost {
	return &TypesHost{
		resources:   resources,
		descriptors: resource.NewTypedTable[*Descriptor](resources.Table(), uint32(TypeDescriptor)),
		dirStreams:  resource.NewTypedTable[*DirectoryEntryStream](resources.Table(), uint32(TypeDirectoryEntryStream)),
	}
}

// Info returns host metadata.
func (h *TypesHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   TypesNamespace,
		Description: "WASI filesystem types and operations",
		Class:       []string{wasmapi.ClassStorage, wasmapi.ClassIO},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *TypesHost) Namespace() string {
	return TypesNamespace
}

// Register returns the host registration.
func (h *TypesHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			// Descriptor methods
			"[method]descriptor.read-via-stream":     h.readViaStream,
			"[method]descriptor.write-via-stream":    h.writeViaStream,
			"[method]descriptor.append-via-stream":   h.appendViaStream,
			"[method]descriptor.advise":              h.advise,
			"[method]descriptor.sync-data":           h.syncData,
			"[method]descriptor.get-flags":           h.getFlags,
			"[method]descriptor.get-type":            h.getType,
			"[method]descriptor.set-size":            h.setSize,
			"[method]descriptor.set-times":           h.setTimes,
			"[method]descriptor.read":                h.read,
			"[method]descriptor.write":               h.write,
			"[method]descriptor.read-directory":      h.readDirectory,
			"[method]descriptor.sync":                h.sync,
			"[method]descriptor.create-directory-at": h.createDirectoryAt,
			"[method]descriptor.stat":                h.stat,
			"[method]descriptor.stat-at":             h.statAt,
			"[method]descriptor.set-times-at":        h.setTimesAt,
			"[method]descriptor.link-at":             h.linkAt,
			"[method]descriptor.open-at":             h.openAt,
			"[method]descriptor.readlink-at":         h.readlinkAt,
			"[method]descriptor.remove-directory-at": h.removeDirectoryAt,
			"[method]descriptor.rename-at":           h.renameAt,
			"[method]descriptor.symlink-at":          h.symlinkAt,
			"[method]descriptor.unlink-file-at":      h.unlinkFileAt,
			"[method]descriptor.is-same-object":      h.isSameObject,
			"[method]descriptor.metadata-hash":       h.metadataHash,
			"[method]descriptor.metadata-hash-at":    h.metadataHashAt,
			"[resource-drop]descriptor":              h.dropDescriptor,

			// Directory entry stream
			"[method]directory-entry-stream.read-directory-entry": h.readDirectoryEntry,
			"[resource-drop]directory-entry-stream":               h.dropDirectoryEntryStream,

			// Standalone functions
			"filesystem-error-code": h.filesystemErrorCode,
		},
	}
}

// Resources returns the shared resource table.
func (h *TypesHost) Resources() *resource.InstanceResources {
	return h.resources
}

// Descriptors returns the typed table for file descriptors.
func (h *TypesHost) Descriptors() *resource.TypedTable[*Descriptor] {
	return h.descriptors
}

// getFS retrieves filesystem from context by ID.
func (h *TypesHost) getFS(ctx context.Context, fsID string) (fsapi.FS, bool) {
	reg := fsapi.GetRegistry(ctx)
	if reg == nil {
		return nil, false
	}
	return reg.GetFS(fsID)
}

// getConfig retrieves filesystem config from frame context.
func (h *TypesHost) getConfig(ctx context.Context) *Config {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		if v, ok := fc.Get(DefaultFSKey); ok {
			if cfg, ok := v.(*Config); ok {
				return cfg
			}
		}
	}
	return nil
}

// resolvePath joins base path with relative path safely.
func resolvePath(base, rel string) string {
	if rel == "" {
		return base
	}
	if len(rel) > 0 && rel[0] == '/' {
		return rel[1:]
	}
	return filepath.Join(base, rel)
}

// Security action constants for filesystem operations
const (
	ActionFSRead   = "wasi.fs.read"
	ActionFSWrite  = "wasi.fs.write"
	ActionFSDelete = "wasi.fs.delete"
	ActionFSStat   = "wasi.fs.stat"
)

// checkSecurity validates both config-level and security context permissions.
func (h *TypesHost) checkSecurity(ctx context.Context, desc *Descriptor, action string) bool {
	cfg := h.getConfig(ctx)
	if cfg != nil && !cfg.IsAllowed(desc.FSID) {
		return false
	}
	meta := registry.Metadata{"fsid": desc.FSID, "path": desc.Path}
	return security.IsAllowed(ctx, action, desc.Path, meta)
}

// Descriptor methods

func (h *TypesHost) readViaStream(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok || desc.IsDir || desc.File == nil {
		stack[0] = 0 // error
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSRead) {
		stack[0] = 0
		return
	}

	// Register the file with StreamRegistry for async reads via dispatcher.
	registry := streamservice.GetOrCreateStreamRegistry(ctx)
	streamID := registry.RegisterStream(desc.File)

	stream := &resource.InputStream{
		StreamID: streamID,
	}
	streamHandle := h.resources.InputStreams().Insert(stream)
	stack[0] = uint64(streamHandle)
}

func (h *TypesHost) writeViaStream(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok || desc.IsDir || desc.File == nil {
		stack[0] = 0
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSWrite) {
		stack[0] = 0
		return
	}

	// Register the file with StreamRegistry for async writes via dispatcher.
	registry := streamservice.GetOrCreateStreamRegistry(ctx)
	streamID := registry.RegisterStream(desc.File)

	stream := &resource.OutputStream{
		StreamID: streamID,
	}
	streamHandle := h.resources.OutputStreams().Insert(stream)
	stack[0] = uint64(streamHandle)
}

func (h *TypesHost) appendViaStream(ctx context.Context, mod api.Module, stack []uint64) {
	// Same as writeViaStream but file should be opened in append mode
	h.writeViaStream(ctx, mod, stack)
}

func (h *TypesHost) advise(ctx context.Context, mod api.Module, stack []uint64) {
	// Advisory hint to kernel - no-op for virtual filesystems
	if len(stack) > 0 {
		stack[0] = 0 // success
	}
}

func (h *TypesHost) syncData(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok {
		stack[0] = uint64(ErrBadDescriptor)
		return
	}
	if desc.File != nil {
		if err := desc.File.Sync(); err != nil {
			stack[0] = uint64(errorToCode(err))
			return
		}
	}
	stack[0] = 0 // success
}

func (h *TypesHost) getFlags(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	if desc, ok := h.descriptors.Get(handle); ok {
		stack[0] = uint64(desc.Flags)
	} else {
		stack[0] = 0
	}
}

func (h *TypesHost) getType(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok {
		stack[0] = uint64(DescTypeUnknown)
		return
	}
	if desc.IsDir {
		stack[0] = uint64(DescTypeDirectory)
	} else {
		stack[0] = uint64(DescTypeRegularFile)
	}
}

func (h *TypesHost) setSize(ctx context.Context, mod api.Module, stack []uint64) {
	// Truncate file - not supported by fsapi.File interface
	if len(stack) > 0 {
		stack[0] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) setTimes(ctx context.Context, mod api.Module, stack []uint64) {
	// Set file times - not supported by fsapi.File interface
	if len(stack) > 0 {
		stack[0] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) read(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 3 {
		return
	}
	handle := resource.Handle(stack[0])
	length := stack[1]
	offset := stack[2]

	desc, ok := h.descriptors.Get(handle)
	if !ok || desc.IsDir || desc.File == nil {
		stack[0] = 0 // error tag
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSRead) {
		stack[0] = 0
		stack[1] = uint64(ErrAccess)
		return
	}

	// Seek to offset
	if _, err := desc.File.Seek(int64(offset), io.SeekStart); err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	// Read data
	buf := make([]byte, length)
	n, err := desc.File.Read(buf)
	if err != nil && err != io.EOF {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	// Write to WASM memory
	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}
	if realloc == nil {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	results, err := realloc.Call(ctx, 0, 0, 1, uint64(n))
	if err != nil || len(results) == 0 {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	ptr := uint32(results[0])
	mem.Write(ptr, buf[:n])

	// Return success with (ptr, len, eof)
	stack[0] = 1 // ok tag
	stack[1] = uint64(ptr)
	if len(stack) > 2 {
		stack[2] = uint64(n)
	}
	if len(stack) > 3 {
		if err == io.EOF {
			stack[3] = 1
		} else {
			stack[3] = 0
		}
	}
}

func (h *TypesHost) write(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 4 {
		return
	}
	handle := resource.Handle(stack[0])
	dataPtr := uint32(stack[1])
	dataLen := uint32(stack[2])
	offset := stack[3]

	desc, ok := h.descriptors.Get(handle)
	if !ok || desc.IsDir || desc.File == nil {
		stack[0] = 0
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSWrite) {
		stack[0] = 0
		stack[1] = uint64(ErrAccess)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	data, ok := mem.Read(dataPtr, dataLen)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	// Seek to offset
	if _, err := desc.File.Seek(int64(offset), io.SeekStart); err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	n, err := desc.File.Write(data)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	stack[0] = 1 // ok
	stack[1] = uint64(n)
}

func (h *TypesHost) readDirectory(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = 0
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSRead) {
		stack[0] = 0
		stack[1] = uint64(ErrAccess)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrNoEntry)
		return
	}

	entries, err := filesystem.ReadDir(desc.Path)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	// Convert to DirectoryEntry list
	dirEntries := make([]DirectoryEntry, len(entries))
	for i, e := range entries {
		dirEntries[i] = DirectoryEntry{
			Name: e.Name(),
		}
		if e.IsDir() {
			dirEntries[i].Type = DescTypeDirectory
		} else {
			dirEntries[i].Type = DescTypeRegularFile
		}
	}

	stream := &DirectoryEntryStream{
		Entries: dirEntries,
		Index:   0,
	}
	streamHandle := h.dirStreams.Insert(stream)

	stack[0] = 1 // ok
	stack[1] = uint64(streamHandle)
}

func (h *TypesHost) sync(ctx context.Context, mod api.Module, stack []uint64) {
	h.syncData(ctx, mod, stack)
}

func (h *TypesHost) createDirectoryAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 3 {
		return
	}
	handle := resource.Handle(stack[0])
	pathPtr := uint32(stack[1])
	pathLen := uint32(stack[2])

	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSWrite) {
		stack[0] = uint64(ErrAccess)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = uint64(ErrIO)
		return
	}

	pathBytes, ok := mem.Read(pathPtr, pathLen)
	if !ok {
		stack[0] = uint64(ErrIO)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = uint64(ErrNoEntry)
		return
	}

	fullPath := resolvePath(desc.Path, string(pathBytes))
	if err := filesystem.Mkdir(fullPath, 0755); err != nil {
		stack[0] = uint64(errorToCode(err))
		return
	}

	stack[0] = 0 // success
}

func (h *TypesHost) stat(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok {
		stack[0] = 0 // error
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSStat) {
		stack[0] = 0
		stack[1] = uint64(ErrAccess)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrNoEntry)
		return
	}

	info, err := filesystem.Stat(desc.Path)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	h.writeStatToStack(info, stack)
}

func (h *TypesHost) statAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 4 {
		return
	}
	handle := resource.Handle(stack[0])
	// stack[1] = path-flags (symlink_follow)
	pathPtr := uint32(stack[2])
	pathLen := uint32(stack[3])

	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = 0
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSStat) {
		stack[0] = 0
		stack[1] = uint64(ErrAccess)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	pathBytes, ok := mem.Read(pathPtr, pathLen)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrNoEntry)
		return
	}

	fullPath := resolvePath(desc.Path, string(pathBytes))
	info, err := filesystem.Stat(fullPath)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	h.writeStatToStack(info, stack)
}

func (h *TypesHost) writeStatToStack(info fs.FileInfo, stack []uint64) {
	// Return: ok tag, then stat fields
	stack[0] = 1 // ok

	descType := DescTypeRegularFile
	if info.IsDir() {
		descType = DescTypeDirectory
	}
	if len(stack) > 1 {
		stack[1] = uint64(descType)
	}
	if len(stack) > 2 {
		stack[2] = 1 // link count
	}
	if len(stack) > 3 {
		stack[3] = uint64(info.Size())
	}
	if len(stack) > 4 {
		stack[4] = uint64(info.ModTime().UnixNano())
	}
	if len(stack) > 5 {
		stack[5] = uint64(info.ModTime().UnixNano())
	}
	if len(stack) > 6 {
		stack[6] = uint64(info.ModTime().UnixNano())
	}
}

func (h *TypesHost) setTimesAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		stack[0] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) linkAt(ctx context.Context, mod api.Module, stack []uint64) {
	// Hard links not supported
	if len(stack) > 0 {
		stack[0] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) openAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 5 {
		return
	}
	handle := resource.Handle(stack[0])
	// stack[1] = path-flags
	pathPtr := uint32(stack[2])
	pathLen := uint32(stack[3])
	openFlags := OpenFlags(stack[4])

	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = 0
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	// Determine action based on open flags
	action := ActionFSRead
	if openFlags&(OpenFlagCreate|OpenFlagTruncate) != 0 {
		action = ActionFSWrite
	}
	if !h.checkSecurity(ctx, desc, action) {
		stack[0] = 0
		stack[1] = uint64(ErrAccess)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	pathBytes, ok := mem.Read(pathPtr, pathLen)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrNoEntry)
		return
	}

	fullPath := resolvePath(desc.Path, string(pathBytes))

	// Convert WASI open flags to Go flags
	var flag int
	if openFlags&OpenFlagCreate != 0 {
		flag |= os.O_CREATE
	}
	if openFlags&OpenFlagExclusive != 0 {
		flag |= os.O_EXCL
	}
	if openFlags&OpenFlagTruncate != 0 {
		flag |= os.O_TRUNC
	}
	if openFlags&OpenFlagDirectory != 0 {
		// Opening a directory
		info, err := filesystem.Stat(fullPath)
		if err != nil {
			stack[0] = 0
			stack[1] = uint64(errorToCode(err))
			return
		}
		if !info.IsDir() {
			stack[0] = 0
			stack[1] = uint64(ErrNotDirectory)
			return
		}

		newDesc := &Descriptor{
			FSID:  desc.FSID,
			Path:  fullPath,
			IsDir: true,
			Flags: FlagRead | FlagMutateDirectory,
		}
		newHandle := h.descriptors.Insert(newDesc)
		stack[0] = 1
		stack[1] = uint64(newHandle)
		return
	}

	// Opening a file
	if flag == 0 {
		flag = os.O_RDONLY
	}

	file, err := filesystem.OpenFile(fullPath, flag, 0644)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	newDesc := &Descriptor{
		FSID:  desc.FSID,
		Path:  fullPath,
		IsDir: false,
		Flags: FlagRead | FlagWrite,
		File:  file,
	}
	newHandle := h.descriptors.Insert(newDesc)

	stack[0] = 1 // ok
	stack[1] = uint64(newHandle)
}

func (h *TypesHost) readlinkAt(ctx context.Context, mod api.Module, stack []uint64) {
	// Symlinks not supported
	if len(stack) > 0 {
		stack[0] = 0
		stack[1] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) removeDirectoryAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 3 {
		return
	}
	handle := resource.Handle(stack[0])
	pathPtr := uint32(stack[1])
	pathLen := uint32(stack[2])

	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSDelete) {
		stack[0] = uint64(ErrAccess)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = uint64(ErrIO)
		return
	}

	pathBytes, ok := mem.Read(pathPtr, pathLen)
	if !ok {
		stack[0] = uint64(ErrIO)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = uint64(ErrNoEntry)
		return
	}

	fullPath := resolvePath(desc.Path, string(pathBytes))

	// Check if empty directory
	entries, err := filesystem.ReadDir(fullPath)
	if err != nil {
		stack[0] = uint64(errorToCode(err))
		return
	}
	if len(entries) > 0 {
		stack[0] = uint64(ErrNotEmpty)
		return
	}

	if err := filesystem.Remove(fullPath); err != nil {
		stack[0] = uint64(errorToCode(err))
		return
	}

	stack[0] = 0 // success
}

func (h *TypesHost) renameAt(ctx context.Context, mod api.Module, stack []uint64) {
	// Rename not directly supported by fsapi.FS
	if len(stack) > 0 {
		stack[0] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) symlinkAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		stack[0] = uint64(ErrUnsupported)
	}
}

func (h *TypesHost) unlinkFileAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 3 {
		return
	}
	handle := resource.Handle(stack[0])
	pathPtr := uint32(stack[1])
	pathLen := uint32(stack[2])

	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = uint64(ErrBadDescriptor)
		return
	}

	if !h.checkSecurity(ctx, desc, ActionFSDelete) {
		stack[0] = uint64(ErrAccess)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = uint64(ErrIO)
		return
	}

	pathBytes, ok := mem.Read(pathPtr, pathLen)
	if !ok {
		stack[0] = uint64(ErrIO)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = uint64(ErrNoEntry)
		return
	}

	fullPath := resolvePath(desc.Path, string(pathBytes))
	if err := filesystem.Remove(fullPath); err != nil {
		stack[0] = uint64(errorToCode(err))
		return
	}

	stack[0] = 0 // success
}

func (h *TypesHost) isSameObject(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 2 {
		return
	}
	h1 := resource.Handle(stack[0])
	h2 := resource.Handle(stack[1])

	d1, ok1 := h.descriptors.Get(h1)
	d2, ok2 := h.descriptors.Get(h2)

	if ok1 && ok2 && d1.FSID == d2.FSID && d1.Path == d2.Path {
		stack[0] = 1 // true
	} else {
		stack[0] = 0 // false
	}
}

func (h *TypesHost) metadataHash(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	desc, ok := h.descriptors.Get(handle)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrNoEntry)
		return
	}

	info, err := filesystem.Stat(desc.Path)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	// Simple hash from mtime and size
	hash := uint64(info.ModTime().UnixNano()) ^ uint64(info.Size())

	stack[0] = 1 // ok
	stack[1] = hash
	if len(stack) > 2 {
		stack[2] = 0 // upper bits
	}
}

func (h *TypesHost) metadataHashAt(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 4 {
		return
	}
	handle := resource.Handle(stack[0])
	pathPtr := uint32(stack[2])
	pathLen := uint32(stack[3])

	desc, ok := h.descriptors.Get(handle)
	if !ok || !desc.IsDir {
		stack[0] = 0
		stack[1] = uint64(ErrBadDescriptor)
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	pathBytes, ok := mem.Read(pathPtr, pathLen)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrIO)
		return
	}

	filesystem, ok := h.getFS(ctx, desc.FSID)
	if !ok {
		stack[0] = 0
		stack[1] = uint64(ErrNoEntry)
		return
	}

	fullPath := resolvePath(desc.Path, string(pathBytes))
	info, err := filesystem.Stat(fullPath)
	if err != nil {
		stack[0] = 0
		stack[1] = uint64(errorToCode(err))
		return
	}

	hash := uint64(info.ModTime().UnixNano()) ^ uint64(info.Size())

	stack[0] = 1 // ok
	stack[1] = hash
	if len(stack) > 2 {
		stack[2] = 0
	}
}

func (h *TypesHost) dropDescriptor(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		h.resources.Table().Remove(handle)
	}
}

func (h *TypesHost) readDirectoryEntry(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	stream, ok := h.dirStreams.Get(handle)
	if !ok {
		stack[0] = 0 // none
		return
	}

	if stream.Index >= len(stream.Entries) {
		stack[0] = 0 // none (end of stream)
		return
	}

	entry := stream.Entries[stream.Index]
	stream.Index++

	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}
	if realloc == nil {
		stack[0] = 0
		return
	}

	nameBytes := []byte(entry.Name)
	results, err := realloc.Call(ctx, 0, 0, 1, uint64(len(nameBytes)))
	if err != nil || len(results) == 0 {
		stack[0] = 0
		return
	}

	ptr := uint32(results[0])
	mem.Write(ptr, nameBytes)

	stack[0] = 1 // some
	if len(stack) > 1 {
		stack[1] = uint64(entry.Type)
	}
	if len(stack) > 2 {
		stack[2] = uint64(ptr)
	}
	if len(stack) > 3 {
		stack[3] = uint64(len(nameBytes))
	}
}

func (h *TypesHost) dropDirectoryEntryStream(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		h.resources.Table().Remove(handle)
	}
}

func (h *TypesHost) filesystemErrorCode(ctx context.Context, mod api.Module, stack []uint64) {
	// Convert I/O error to filesystem error code
	// In practice, this would look up the last error
	if len(stack) > 0 {
		stack[0] = 1 // some
		stack[1] = uint64(ErrIO)
	}
}

// Descriptor represents a WASI filesystem descriptor.
type Descriptor struct {
	FSID  string // Filesystem ID in registry
	Path  string // Path within filesystem
	IsDir bool
	Flags DescriptorFlags
	File  fsapi.File // Open file handle (nil for directories)
}

// Drop implements resource.Dropper.
func (d *Descriptor) Drop() {
	if d.File != nil {
		d.File.Close()
		d.File = nil
	}
	d.Path = ""
}

// DescriptorFlags represents WASI descriptor flags.
type DescriptorFlags uint32

const (
	FlagRead DescriptorFlags = 1 << iota
	FlagWrite
	FlagFileIntegritySync
	FlagDataIntegritySync
	FlagRequestedWriteSync
	FlagMutateDirectory
)

// OpenFlags represents WASI open flags.
type OpenFlags uint32

const (
	OpenFlagCreate OpenFlags = 1 << iota
	OpenFlagDirectory
	OpenFlagExclusive
	OpenFlagTruncate
)

// DescriptorType represents WASI descriptor type.
type DescriptorType uint8

const (
	DescTypeUnknown DescriptorType = iota
	DescTypeBlockDevice
	DescTypeCharacterDevice
	DescTypeDirectory
	DescTypeFIFO
	DescTypeSymbolicLink
	DescTypeRegularFile
	DescTypeSocket
)

// ErrorCode represents WASI filesystem error codes.
type ErrorCode uint8

const (
	ErrAccess ErrorCode = iota + 1
	ErrWouldBlock
	ErrAlready
	ErrBadDescriptor
	ErrBusy
	ErrDeadlock
	ErrQuota
	ErrExist
	ErrFileTooLarge
	ErrIllegalByteSequence
	ErrInProgress
	ErrInterrupted
	ErrInvalid
	ErrIO
	ErrIsDirectory
	ErrLoop
	ErrTooManyLinks
	ErrMessageSize
	ErrNameTooLong
	ErrNoDevice
	ErrNoEntry
	ErrNoLock
	ErrInsufficientMemory
	ErrInsufficientSpace
	ErrNotDirectory
	ErrNotEmpty
	ErrNotRecoverable
	ErrUnsupported
	ErrNoTTY
	ErrNoSuchDevice
	ErrOverflow
	ErrNotPermitted
	ErrPipe
	ErrReadOnly
	ErrInvalidSeek
	ErrTextFileBusy
	ErrCrossDevice
)

// errorToCode converts Go errors to WASI error codes.
func errorToCode(err error) ErrorCode {
	if err == nil {
		return 0
	}
	if os.IsNotExist(err) {
		return ErrNoEntry
	}
	if os.IsExist(err) {
		return ErrExist
	}
	if os.IsPermission(err) {
		return ErrAccess
	}
	return ErrIO
}

// DirectoryEntry represents a directory entry.
type DirectoryEntry struct {
	Type DescriptorType
	Name string
}

// DirectoryEntryStream holds state for reading directory entries.
type DirectoryEntryStream struct {
	Entries []DirectoryEntry
	Index   int
}

// Drop implements resource.Dropper.
func (s *DirectoryEntryStream) Drop() {
	s.Entries = nil
}

// Stat represents file metadata.
type Stat struct {
	Type             DescriptorType
	LinkCount        uint64
	Size             uint64
	DataAccessTime   time.Time
	DataModTime      time.Time
	StatusChangeTime time.Time
}

// Compile-time check
var _ wasmapi.Host = (*TypesHost)(nil)
