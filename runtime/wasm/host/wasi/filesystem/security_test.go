package filesystem

import (
	"context"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

// mockFile implements fsapi.File for testing
type mockFile struct{}

func (f *mockFile) Read(p []byte) (int, error)                   { return 0, io.EOF }
func (f *mockFile) Write(p []byte) (int, error)                  { return len(p), nil }
func (f *mockFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *mockFile) Close() error                                 { return nil }
func (f *mockFile) Sync() error                                  { return nil }
func (f *mockFile) Stat() (fs.FileInfo, error)                   { return &mockFileInfo{}, nil }

type mockFileInfo struct{}

func (i *mockFileInfo) Name() string       { return "mock" }
func (i *mockFileInfo) Size() int64        { return 0 }
func (i *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (i *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (i *mockFileInfo) IsDir() bool        { return false }
func (i *mockFileInfo) Sys() any           { return nil }

var _ fsapi.File = (*mockFile)(nil)

// mockPolicy implements security.Policy for testing
type mockPolicy struct {
	id           registry.ID
	allowActions map[string]bool
}

func (p *mockPolicy) ID() registry.ID {
	return p.id
}

func (p *mockPolicy) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	if allowed, ok := p.allowActions[action]; ok && allowed {
		return security.Allow
	}
	return security.Deny
}

// mockScope implements security.Scope for testing
type mockScope struct {
	policies []security.Policy
}

func newMockScope(allowActions ...string) *mockScope {
	actionMap := make(map[string]bool)
	for _, a := range allowActions {
		actionMap[a] = true
	}
	return &mockScope{
		policies: []security.Policy{&mockPolicy{
			id:           registry.NewID("test", "policy"),
			allowActions: actionMap,
		}},
	}
}

func (s *mockScope) With(policy security.Policy) security.Scope {
	return &mockScope{policies: append(s.policies, policy)}
}

func (s *mockScope) Without(policyID registry.ID) security.Scope {
	return s
}

func (s *mockScope) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	for _, p := range s.policies {
		if result := p.Evaluate(actor, action, resource, meta); result == security.Allow {
			return security.Allow
		}
	}
	return security.Deny
}

func (s *mockScope) Contains(policyID registry.ID) bool {
	return false
}

func (s *mockScope) Policies() []security.Policy {
	return s.policies
}

// setupSecurityContext creates a context with security actor and scope
func setupSecurityContext(allowActions ...string) context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	security.SetActor(ctx, security.Actor{ID: "test-user"})
	security.SetScope(ctx, newMockScope(allowActions...))
	return ctx
}

// setupDeniedContext creates a context where all actions are denied
func setupDeniedContext() context.Context {
	return setupSecurityContext() // no allowed actions
}

func TestSecurityCheckSecurity(t *testing.T) {
	t.Run("denies when no security context", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		// No frame context - security should deny
		ctx := context.Background()
		desc := &Descriptor{FSID: "local", Path: "/test"}

		allowed := host.checkSecurity(ctx, desc, ActionFSRead)
		assert.False(t, allowed, "should deny without security context")
	})

	t.Run("denies when action not allowed", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		ctx := setupSecurityContext(ActionFSRead) // only read allowed
		desc := &Descriptor{FSID: "local", Path: "/test"}

		assert.True(t, host.checkSecurity(ctx, desc, ActionFSRead), "read should be allowed")
		assert.False(t, host.checkSecurity(ctx, desc, ActionFSWrite), "write should be denied")
		assert.False(t, host.checkSecurity(ctx, desc, ActionFSDelete), "delete should be denied")
	})

	t.Run("allows when action permitted", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		ctx := setupSecurityContext(ActionFSRead, ActionFSWrite, ActionFSDelete, ActionFSStat)
		desc := &Descriptor{FSID: "local", Path: "/test"}

		assert.True(t, host.checkSecurity(ctx, desc, ActionFSRead))
		assert.True(t, host.checkSecurity(ctx, desc, ActionFSWrite))
		assert.True(t, host.checkSecurity(ctx, desc, ActionFSDelete))
		assert.True(t, host.checkSecurity(ctx, desc, ActionFSStat))
	})

	t.Run("denies when config disallows filesystem", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		// Create context with allowed security but restricted config
		ctx := setupSecurityContext(ActionFSRead, ActionFSWrite)
		fc := ctxapi.FrameFromContext(ctx)
		cfg := &Config{AllowedFS: []string{"temp"}} // only temp allowed
		fc.Set(DefaultFSKey, cfg)

		desc := &Descriptor{FSID: "local", Path: "/test"} // local not in allowed list

		assert.False(t, host.checkSecurity(ctx, desc, ActionFSRead), "should deny - fsid not in allowed list")
	})
}

func TestSecurityRead(t *testing.T) {
	t.Run("denies read without permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		// Insert a file descriptor with a mock file (uses mockFile)
		desc := &Descriptor{FSID: "local", Path: "/test.txt", IsDir: false, File: &mockFile{}}
		handle := host.descriptors.Insert(desc)

		ctx := setupDeniedContext()
		stack := []uint64{uint64(handle), 100, 0} // handle, length, offset

		host.read(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})
}

func TestSecurityWrite(t *testing.T) {
	t.Run("denies write without permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/test.txt", IsDir: false, File: &mockFile{}}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead)  // only read, not write
		stack := []uint64{uint64(handle), 0, 0, 0} // handle, ptr, len, offset

		host.write(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})
}

func TestSecurityReadDirectory(t *testing.T) {
	t.Run("denies readDirectory without permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/dir", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupDeniedContext()
		stack := []uint64{uint64(handle), 0}

		host.readDirectory(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})
}

func TestSecurityStat(t *testing.T) {
	t.Run("denies stat without permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/test.txt", IsDir: false}
		handle := host.descriptors.Insert(desc)

		ctx := setupDeniedContext()
		stack := []uint64{uint64(handle), 0}

		host.stat(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})
}

func TestSecurityStatAt(t *testing.T) {
	t.Run("denies statAt without permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/dir", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupDeniedContext()
		stack := []uint64{uint64(handle), 0, 0, 0}

		host.statAt(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})
}

func TestSecurityCreateDirectoryAt(t *testing.T) {
	t.Run("denies createDirectoryAt without write permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/parent", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead) // only read, not write
		stack := []uint64{uint64(handle), 0, 0}

		host.createDirectoryAt(ctx, nil, stack)

		assert.Equal(t, uint64(ErrAccess), stack[0], "should return ErrAccess")
	})
}

func TestSecurityRemoveDirectoryAt(t *testing.T) {
	t.Run("denies removeDirectoryAt without delete permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/parent", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead, ActionFSWrite) // no delete
		stack := []uint64{uint64(handle), 0, 0}

		host.removeDirectoryAt(ctx, nil, stack)

		assert.Equal(t, uint64(ErrAccess), stack[0], "should return ErrAccess")
	})
}

func TestSecurityUnlinkFileAt(t *testing.T) {
	t.Run("denies unlinkFileAt without delete permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/parent", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead, ActionFSWrite) // no delete
		stack := []uint64{uint64(handle), 0, 0}

		host.unlinkFileAt(ctx, nil, stack)

		assert.Equal(t, uint64(ErrAccess), stack[0], "should return ErrAccess")
	})
}

func TestSecurityOpenAt(t *testing.T) {
	t.Run("denies openAt for create without write permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/dir", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead) // only read, not write
		// OpenFlagCreate = 1
		stack := []uint64{uint64(handle), 0, 0, 0, 1}

		host.openAt(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})

	t.Run("denies openAt for truncate without write permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/dir", IsDir: true}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead) // only read, not write
		// OpenFlagTruncate = 8
		stack := []uint64{uint64(handle), 0, 0, 0, 8}

		host.openAt(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return error tag")
		assert.Equal(t, uint64(ErrAccess), stack[1], "should return ErrAccess")
	})
}

func TestSecurityReadViaStream(t *testing.T) {
	t.Run("denies readViaStream without read permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/test.txt", IsDir: false}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSWrite) // only write, not read
		stack := []uint64{uint64(handle)}

		host.readViaStream(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return 0 (error)")
	})
}

func TestSecurityWriteViaStream(t *testing.T) {
	t.Run("denies writeViaStream without write permission", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		desc := &Descriptor{FSID: "local", Path: "/test.txt", IsDir: false}
		handle := host.descriptors.Insert(desc)

		ctx := setupSecurityContext(ActionFSRead) // only read, not write
		stack := []uint64{uint64(handle)}

		host.writeViaStream(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return 0 (error)")
	})
}

func TestSecurityActionConstants(t *testing.T) {
	// Verify action constants are properly defined
	assert.Equal(t, "wasi.fs.read", ActionFSRead)
	assert.Equal(t, "wasi.fs.write", ActionFSWrite)
	assert.Equal(t, "wasi.fs.delete", ActionFSDelete)
	assert.Equal(t, "wasi.fs.stat", ActionFSStat)
}

func TestSecurityMetadata(t *testing.T) {
	t.Run("checkSecurity includes fsid and path in metadata", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		// Create a custom scope that captures the metadata
		capturedMeta := make(registry.Metadata)
		captureScope := &captureMetadataScope{captured: &capturedMeta}

		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		security.SetActor(ctx, security.Actor{ID: "test-user"})
		security.SetScope(ctx, captureScope)

		desc := &Descriptor{FSID: "test-fs", Path: "/app/data"}
		host.checkSecurity(ctx, desc, ActionFSRead)

		assert.Equal(t, "test-fs", capturedMeta["fsid"])
		assert.Equal(t, "/app/data", capturedMeta["path"])
	})
}

// captureMetadataScope captures metadata during Evaluate for inspection
type captureMetadataScope struct {
	captured *registry.Metadata
}

func (s *captureMetadataScope) With(policy security.Policy) security.Scope  { return s }
func (s *captureMetadataScope) Without(policyID registry.ID) security.Scope { return s }
func (s *captureMetadataScope) Contains(policyID registry.ID) bool          { return false }
func (s *captureMetadataScope) Policies() []security.Policy                 { return nil }
func (s *captureMetadataScope) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	for k, v := range meta {
		(*s.captured)[k] = v
	}
	return security.Allow
}

func TestSecurityIntegration(t *testing.T) {
	t.Run("config and security both checked", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewTypesHost(res)

		// Security allows, but config denies
		ctx := setupSecurityContext(ActionFSRead)
		fc := ctxapi.FrameFromContext(ctx)
		require.NotNil(t, fc)

		cfg := &Config{AllowedFS: []string{"allowed-fs"}}
		fc.Set(DefaultFSKey, cfg)

		desc := &Descriptor{FSID: "disallowed-fs", Path: "/test"}
		assert.False(t, host.checkSecurity(ctx, desc, ActionFSRead))

		// Config allows, but security denies
		desc2 := &Descriptor{FSID: "allowed-fs", Path: "/test"}
		ctx2 := setupSecurityContext() // empty - no actions allowed
		fc2 := ctxapi.FrameFromContext(ctx2)
		fc2.Set(DefaultFSKey, cfg)

		assert.False(t, host.checkSecurity(ctx2, desc2, ActionFSRead))

		// Both allow
		ctx3 := setupSecurityContext(ActionFSRead)
		fc3 := ctxapi.FrameFromContext(ctx3)
		fc3.Set(DefaultFSKey, cfg)
		desc3 := &Descriptor{FSID: "allowed-fs", Path: "/test"}

		assert.True(t, host.checkSecurity(ctx3, desc3, ActionFSRead))
	})
}
