// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	netsystem "github.com/wippyai/runtime/system/net"
	payloadsystem "github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

// --- Fakes for driver-dispatch tests ---

type fakeDriver struct {
	kind      registry.Kind
	createFn  func(context.Context, registry.Entry, Deps) (netapi.Service, error)
	createErr error
	lastDeps  Deps
	calls     int
}

func (f *fakeDriver) Kind() registry.Kind { return f.kind }

func (f *fakeDriver) Create(ctx context.Context, entry registry.Entry, deps Deps) (netapi.Service, error) {
	f.calls++
	f.lastDeps = deps
	if f.createFn != nil {
		return f.createFn(ctx, entry, deps)
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &stubService{}, nil
}

type stubService struct{}

func (*stubService) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, netapi.ErrNotSupported
}
func (*stubService) Listen(context.Context, string, string) (net.Listener, error) {
	return nil, netapi.ErrNotSupported
}
func (*stubService) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, netapi.ErrNotSupported
}
func (*stubService) LookupHost(context.Context, string) ([]string, error) {
	return nil, netapi.ErrNotSupported
}

func newTestManager(t *testing.T, opts ...Option) (*Manager, *netsystem.Registry) {
	t.Helper()
	reg := netsystem.NewRegistry(zap.NewNop())
	m, err := NewManager(reg, payloadsystem.NewTranscoder(), nil, zap.NewNop(), opts...)
	require.NoError(t, err)
	return m, reg
}

// --- Deps.DriverStateDir ---

func TestDeps_DriverStateDir_Empty(t *testing.T) {
	d := Deps{}
	assert.Equal(t, "", d.DriverStateDir("tailscale", "node1"))
	assert.Equal(t, "", d.DriverStateDir("tailscale", ""))
}

func TestDeps_DriverStateDir_WithBase(t *testing.T) {
	d := Deps{StateDir: "/var/lib/wippy/net"}

	assert.Equal(t,
		filepath.Join("/var/lib/wippy/net", "tailscale", "worker"),
		d.DriverStateDir("tailscale", "worker"),
	)
	assert.Equal(t,
		filepath.Join("/var/lib/wippy/net", "tailscale"),
		d.DriverStateDir("tailscale", ""),
	)
}

// --- Option behavior ---

func TestWithStateDir_Applies(t *testing.T) {
	m, _ := newTestManager(t, WithStateDir("/tmp/wippy-net"))
	assert.Equal(t, "/tmp/wippy-net", m.deps.StateDir)
}

func TestWithDriver_RegistersByKind(t *testing.T) {
	d := &fakeDriver{kind: "network.fake"}
	m, _ := newTestManager(t, WithDriver(d))

	assert.Same(t, d, m.drivers["network.fake"])
}

func TestWithDriver_NilIgnored(t *testing.T) {
	m, _ := newTestManager(t, WithDriver(nil))
	assert.Empty(t, m.drivers)
}

func TestWithDriver_LaterReplacesEarlier(t *testing.T) {
	d1 := &fakeDriver{kind: "network.fake"}
	d2 := &fakeDriver{kind: "network.fake"}
	m, _ := newTestManager(t, WithDriver(d1, d2))

	assert.Same(t, d2, m.drivers["network.fake"])
}

// --- Dispatch ---

func TestManager_Add_UnknownKind_ReturnsError(t *testing.T) {
	m, _ := newTestManager(t)

	err := m.Add(context.Background(), registry.Entry{
		ID:   registry.NewID("app.net", "x"),
		Kind: "network.unknown",
	})
	require.Error(t, err)
}

func TestManager_Add_DispatchesToMatchingDriver(t *testing.T) {
	d := &fakeDriver{kind: "network.fake"}
	m, reg := newTestManager(t, WithDriver(d))

	id := registry.NewID("app.net", "x")
	err := m.Add(context.Background(), registry.Entry{ID: id, Kind: "network.fake"})
	require.NoError(t, err)
	assert.Equal(t, 1, d.calls)

	assert.True(t, reg.HasNetwork(id), "successful Create must register the service")
}

func TestManager_Add_DriverErrorDoesNotRegister(t *testing.T) {
	boom := errors.New("driver boom")
	d := &fakeDriver{kind: "network.fake", createErr: boom}
	m, reg := newTestManager(t, WithDriver(d))

	id := registry.NewID("app.net", "x")
	err := m.Add(context.Background(), registry.Entry{ID: id, Kind: "network.fake"})
	require.ErrorIs(t, err, boom)
	assert.False(t, reg.HasNetwork(id))
}

func TestManager_Update_FailedCreate_PreservesPrevious(t *testing.T) {
	// Atomic-swap semantics: Update creates the replacement first and only
	// swaps it in if the driver returns a valid service. A bad config must
	// not tear down a running overlay — in-flight traffic keeps working.
	d := &fakeDriver{kind: "network.fake"}
	m, reg := newTestManager(t, WithDriver(d))

	id := registry.NewID("app.net", "x")
	require.NoError(t, m.Add(context.Background(), registry.Entry{ID: id, Kind: "network.fake"}))
	original, err := reg.GetNetwork(id)
	require.NoError(t, err)
	require.NotNil(t, original)

	d.createErr = errors.New("refreshed driver refuses")
	err = m.Update(context.Background(), registry.Entry{ID: id, Kind: "network.fake"})
	require.Error(t, err, "Update must surface the Create error")

	current, err := reg.GetNetwork(id)
	require.NoError(t, err, "previous service must remain registered")
	assert.Same(t, original, current, "previous service instance must be preserved")
}

func TestManager_Update_Success_ReplacesPrevious(t *testing.T) {
	// Happy-path hot-reload: a successful Create fully replaces the
	// previous service and registry readers see the new instance.
	d := &fakeDriver{kind: "network.fake"}
	m, reg := newTestManager(t, WithDriver(d))

	id := registry.NewID("app.net", "x")
	require.NoError(t, m.Add(context.Background(), registry.Entry{ID: id, Kind: "network.fake"}))
	original, err := reg.GetNetwork(id)
	require.NoError(t, err)

	replacement := &tagService{tag: "replacement"}
	d.createFn = func(context.Context, registry.Entry, Deps) (netapi.Service, error) {
		return replacement, nil
	}
	require.NoError(t, m.Update(context.Background(), registry.Entry{ID: id, Kind: "network.fake"}))

	current, err := reg.GetNetwork(id)
	require.NoError(t, err)
	assert.Same(t, replacement, current)
	assert.NotSame(t, original, current)
}

// tagService is a netapi.Service whose pointer identity is stable across
// allocations (the tag field ensures non-zero struct size).
type tagService struct {
	tag string
}

func (*tagService) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, netapi.ErrNotSupported
}
func (*tagService) Listen(context.Context, string, string) (net.Listener, error) {
	return nil, netapi.ErrNotSupported
}
func (*tagService) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, netapi.ErrNotSupported
}
func (*tagService) LookupHost(context.Context, string) ([]string, error) {
	return nil, netapi.ErrNotSupported
}

func TestManager_Delete_Unregisters(t *testing.T) {
	d := &fakeDriver{kind: "network.fake"}
	m, reg := newTestManager(t, WithDriver(d))

	id := registry.NewID("app.net", "x")
	require.NoError(t, m.Add(context.Background(), registry.Entry{ID: id, Kind: "network.fake"}))
	require.True(t, reg.HasNetwork(id))

	require.NoError(t, m.Delete(context.Background(), registry.Entry{ID: id, Kind: "network.fake"}))
	assert.False(t, reg.HasNetwork(id))
}

func TestManager_Create_ReceivesDepsFromManager(t *testing.T) {
	d := &fakeDriver{kind: "network.fake"}
	m, _ := newTestManager(t, WithDriver(d), WithStateDir("/var/state"))

	id := registry.NewID("app.net", "x")
	require.NoError(t, m.Add(context.Background(), registry.Entry{ID: id, Kind: "network.fake"}))

	assert.Equal(t, "/var/state", d.lastDeps.StateDir)
	assert.NotNil(t, d.lastDeps.Transcoder)
	assert.NotNil(t, d.lastDeps.Logger)
}

// --- NewManager validation ---

func TestNewManager_RejectsNilRegistry(t *testing.T) {
	_, err := NewManager(nil, payloadsystem.NewTranscoder(), nil, zap.NewNop())
	require.Error(t, err)
}

func TestNewManager_RejectsNilTranscoder(t *testing.T) {
	reg := netsystem.NewRegistry(zap.NewNop())
	_, err := NewManager(reg, nil, nil, zap.NewNop())
	require.Error(t, err)
}

func TestNewManager_NilLoggerDefaultsToNop(t *testing.T) {
	reg := netsystem.NewRegistry(zap.NewNop())
	m, err := NewManager(reg, payloadsystem.NewTranscoder(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, m.log)
}
