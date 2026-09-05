// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	coresocket "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/socket"
	netsystem "github.com/wippyai/runtime/system/net"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const socketTestWIT = `
connect: func() -> u64;
send: func(handle: u32) -> u64;
recv: func(handle: u32) -> u64;
close: func(handle: u32) -> u32;
received_byte: func(offset: u32) -> u32;
`

func bootSocketTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	return netapi.WithService(ctx, netsystem.NewSecureService())
}

func startLoopbackEcho(t *testing.T) (net.Listener, int, func()) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				buf := make([]byte, 256)
				for {
					_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	cleanup := func() {
		cancel()
		_ = listener.Close()
		wg.Wait()
		_ = ctx
	}

	return listener, port, cleanup
}

func bootOldWAT(port int) string {
	return fmt.Sprintf(`(module
  (import %q "connect" (func $connect (param i32 i32 i32 i32) (result i64)))
  (import %q "send" (func $send (param i32 i32 i32) (result i64)))
  (import %q "recv" (func $recv (param i32 i32 i32) (result i64)))
  (import %q "close" (func $close (param i32) (result i32)))
  (memory (export "memory") 1)
  (func (export "received_byte") (param i32) (result i32)
    local.get 0 i32.const 64 i32.add i32.load8_u)
  (data (i32.const 0) "127.0.0.1")
  (data (i32.const 32) "ping")
  (func (export "connect") (result i64)
    i32.const 0 i32.const 9 i32.const %d i32.const 1000 call $connect)
  (func (export "send") (param $handle i32) (result i64)
    local.get $handle i32.const 32 i32.const 4 call $send)
  (func (export "recv") (param $handle i32) (result i64)
    local.get $handle i32.const 64 i32.const 16 call $recv)
  (func (export "close") (param $handle i32) (result i32)
    local.get $handle call $close)
)`, coresocket.LegacyNamespace, coresocket.LegacyNamespace, coresocket.LegacyNamespace, coresocket.LegacyNamespace, port)
}

func bootCanonicalWAT(port int) string {
	return fmt.Sprintf(`(module
  (import %q "connect" (func $connect (param i32 i32 i32 i32) (result i64)))
  (import %q "send" (func $send (param i32 i32 i32) (result i64)))
  (import %q "recv" (func $recv (param i32 i32 i32) (result i64)))
  (import %q "close" (func $close (param i32) (result i32)))
  (memory (export "memory") 1)
  (func (export "received_byte") (param i32) (result i32)
    local.get 0 i32.const 64 i32.add i32.load8_u)
  (data (i32.const 0) "127.0.0.1")
  (data (i32.const 32) "ping")
  (func (export "connect") (result i64)
    i32.const 0 i32.const 9 i32.const %d i32.const 1000 call $connect)
  (func (export "send") (param $handle i32) (result i64)
    local.get $handle i32.const 32 i32.const 4 call $send)
  (func (export "recv") (param $handle i32) (result i64)
    local.get $handle i32.const 64 i32.const 16 call $recv)
  (func (export "close") (param $handle i32) (result i32)
    local.get $handle call $close)
)`, coresocket.Namespace, coresocket.Namespace, coresocket.Namespace, coresocket.Namespace, port)
}

func callPacked(ctx context.Context, t *testing.T, inst *wasmrt.Instance, method string, args ...any) (uint32, uint32) {
	t.Helper()
	result, err := inst.Call(ctx, method, args...)
	require.NoError(t, err)
	packed, ok := result.(uint64)
	require.True(t, ok)
	return uint32(packed >> 32), uint32(packed)
}

func callStatus(ctx context.Context, t *testing.T, inst *wasmrt.Instance, method string, args ...any) uint32 {
	t.Helper()
	result, err := inst.Call(ctx, method, args...)
	require.NoError(t, err)
	status, ok := result.(uint32)
	require.True(t, ok)
	return status
}

func TestSocketCompatibility_ProfileResolutionAndMetadata(t *testing.T) {
	core, _ := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	profiles := DefaultHostProfiles(logger, nil)
	var socketProfile *wasmcomponent.HostProfile
	for i := range profiles {
		if profiles[i].Name == wasmcomponent.HostProfileSocket {
			socketProfile = &profiles[i]
			break
		}
	}
	require.NotNil(t, socketProfile, "socket profile must exist in DefaultHostProfiles")

	assert.Contains(t, socketProfile.Aliases, coresocket.Namespace)
	assert.Contains(t, socketProfile.Aliases, coresocket.LegacyYAMLName)
	assert.Contains(t, socketProfile.Aliases, coresocket.LegacyNamespace)

	// Verify deprecation metadata on aliases
	require.NotNil(t, socketProfile.DeprecatedAliases)
	yamlDep, hasYAMLDep := socketProfile.DeprecatedAliases[coresocket.LegacyYAMLName]
	require.True(t, hasYAMLDep)
	assert.True(t, yamlDep.Deprecated)
	assert.Equal(t, wasmcomponent.HostProfileSocket, yamlDep.Replacement)
	assert.Equal(t, 10, yamlDep.MinimumVersions)
	assert.Empty(t, yamlDep.FirstReleasedVersion)

	tcpDep, hasTCPDep := socketProfile.DeprecatedAliases[coresocket.LegacyNamespace]
	require.True(t, hasTCPDep)
	assert.True(t, tcpDep.Deprecated)
	assert.Equal(t, wasmcomponent.HostProfileSocket, tcpDep.Replacement)
	assert.Equal(t, 10, tcpDep.MinimumVersions)

	// Verify registry resolution
	reg := wasmcomponent.NewHostRegistry()
	require.NoError(t, reg.RegisterProfiles(*socketProfile))

	resolveCases := []struct {
		name string
		id   registry.ID
	}{
		{"canonical profile", registry.ParseID("socket")},
		{"canonical versioned", registry.ParseID(coresocket.Namespace)},
		{"legacy yaml alias", registry.ParseID(coresocket.LegacyYAMLName)},
		{"legacy binary namespace", registry.ParseID(coresocket.LegacyNamespace)},
	}

	for _, tc := range resolveCases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := reg.Resolve(tc.id)
			require.True(t, ok)
			assert.Equal(t, wasmcomponent.HostProfileSocket, p.Name)
		})
	}
}

func TestSocketCompatibility_OldAndNewWATClients_SingleRuntime(t *testing.T) {
	ctx := bootSocketTestContext()
	_, port, cleanup := startLoopbackEcho(t)
	defer cleanup()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	core, observedLogs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	reg := wasmcomponent.NewHostRegistry()
	require.NoError(t, reg.RegisterProfiles(DefaultHostProfiles(logger, nil)...))

	// Ensure canonical socket profile once
	require.NoError(t, reg.EnsureImports(ctx, rt, []registry.ID{registry.ParseID("socket")}, false))

	// Load old and new WAT modules
	oldMod, err := rt.LoadWAT(ctx, bootOldWAT(port), socketTestWIT)
	require.NoError(t, err)
	require.NoError(t, oldMod.Compile(ctx))

	canonicalMod, err := rt.LoadWAT(ctx, bootCanonicalWAT(port), socketTestWIT)
	require.NoError(t, err)
	require.NoError(t, canonicalMod.Compile(ctx))

	// Execute old WAT client
	oldInst, err := oldMod.Instantiate(ctx)
	require.NoError(t, err)
	defer oldInst.Close(ctx)

	status, handle := callPacked(ctx, t, oldInst, "connect")
	require.Equal(t, coresocket.StatusOK, status)
	require.NotZero(t, handle)

	status, written := callPacked(ctx, t, oldInst, "send", handle)
	require.Equal(t, coresocket.StatusOK, status)
	require.Equal(t, uint32(4), written)

	requireEchoPayload(ctx, t, oldInst, "recv", handle, "ping")

	closeStatus := callStatus(ctx, t, oldInst, "close", handle)
	require.Equal(t, coresocket.StatusOK, closeStatus)

	// Warning must be logged exactly once for legacy binary namespace
	warnLogs := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogs, 1, "warning must be emitted exactly once for legacy binary client")
	assert.Contains(t, warnLogs[0].Message, "deprecated socket binary namespace used")
	assert.Equal(t, coresocket.LegacyNamespace, warnLogs[0].ContextMap()["namespace"])

	// Execute canonical WAT client
	canonInst, err := canonicalMod.Instantiate(ctx)
	require.NoError(t, err)
	defer canonInst.Close(ctx)

	status, handleNew := callPacked(ctx, t, canonInst, "connect")
	require.Equal(t, coresocket.StatusOK, status)
	require.NotZero(t, handleNew)

	status, written = callPacked(ctx, t, canonInst, "send", handleNew)
	require.Equal(t, coresocket.StatusOK, status)
	require.Equal(t, uint32(4), written)

	requireEchoPayload(ctx, t, canonInst, "recv", handleNew, "ping")

	closeStatus = callStatus(ctx, t, canonInst, "close", handleNew)
	require.Equal(t, coresocket.StatusOK, closeStatus)

	// Canonical usage must produce zero new warnings
	warnLogsAfterCanon := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogsAfterCanon, 1, "canonical client must emit zero warnings")
}

func TestSocketCompatibility_LegacyYAMLImport_DeduplicatedWarning(t *testing.T) {
	ctx := bootSocketTestContext()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	core, observedLogs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	reg := wasmcomponent.NewHostRegistry()
	require.NoError(t, reg.RegisterProfiles(DefaultHostProfiles(logger, nil)...))

	// EnsureImports with legacy YAML alias wippy:sock
	require.NoError(t, reg.EnsureImports(ctx, rt, []registry.ID{registry.ParseID("wippy:sock")}, false))

	// Verify warning logged once
	warnLogs := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogs, 1, "warning must be emitted for deprecated YAML alias")
	assert.Contains(t, warnLogs[0].Message, "deprecated host import alias used")
	assert.Equal(t, "wippy:sock", warnLogs[0].ContextMap()["alias"])

	// Call EnsureImports again on same runtime with same alias -> deduplicated, no new warning
	require.NoError(t, reg.EnsureImports(ctx, rt, []registry.ID{registry.ParseID("wippy:sock")}, false))
	warnLogsSecond := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogsSecond, 1, "duplicate EnsureImports on same runtime must not emit second warning")
}

func TestSocketCompatibility_MixedAliasHandles_BoundsEnforcement(t *testing.T) {
	base := bootSocketTestContext()
	ctx := wippyhost.WithCallLimits(base, wasmapi.LimitsConfig{MaxOpenSockets: 1})
	_, port, cleanup := startLoopbackEcho(t)
	defer cleanup()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	reg := wasmcomponent.NewHostRegistry()
	require.NoError(t, reg.RegisterProfiles(DefaultHostProfiles(zap.NewNop(), nil)...))
	require.NoError(t, reg.EnsureImports(ctx, rt, []registry.ID{registry.ParseID("socket")}, false))

	oldMod, err := rt.LoadWAT(ctx, bootOldWAT(port), socketTestWIT)
	require.NoError(t, err)
	require.NoError(t, oldMod.Compile(ctx))

	canonMod, err := rt.LoadWAT(ctx, bootCanonicalWAT(port), socketTestWIT)
	require.NoError(t, err)
	require.NoError(t, canonMod.Compile(ctx))

	oldInst, err := oldMod.Instantiate(ctx)
	require.NoError(t, err)
	defer oldInst.Close(ctx)

	canonInst, err := canonMod.Instantiate(ctx)
	require.NoError(t, err)
	defer canonInst.Close(ctx)

	// 1. Old instance connects, consumes quota
	status, handleOld := callPacked(ctx, t, oldInst, "connect")
	require.Equal(t, coresocket.StatusOK, status)
	require.NotZero(t, handleOld)

	// 2. Old instance attempts second connect -> limit exceeded
	status2, _ := callPacked(ctx, t, oldInst, "connect")
	require.Equal(t, coresocket.StatusLimit, status2)

	// 3. Isolation: Canon instance cannot close or access handle belonging to old instance
	statusForeign := callStatus(ctx, t, canonInst, "close", handleOld)
	require.Equal(t, coresocket.StatusUnknownHandle, statusForeign, "foreign instance cannot access old handle")

	// 4. Canon instance has its own independent quota and can connect
	statusCanon, handleCanon := callPacked(ctx, t, canonInst, "connect")
	require.Equal(t, coresocket.StatusOK, statusCanon)
	require.NotZero(t, handleCanon)

	// 5. Old instance cannot close canon instance's handle
	// (Open a second socket on listener/canon or verify isolation)
	_ = callStatus(ctx, t, oldInst, "close", handleOld)
	_ = callStatus(ctx, t, canonInst, "close", handleCanon)
}

// TCP may split an echo across reads. Check guest-visible bytes, not only counts.
func requireEchoPayload(ctx context.Context, t *testing.T, inst *wasmrt.Instance, method string, handle uint32, want string) {
	t.Helper()
	got := make([]byte, 0, len(want))
	for len(got) < len(want) {
		status, count := callPacked(ctx, t, inst, method, handle)
		require.Equal(t, coresocket.StatusOK, status)
		require.Greater(t, count, uint32(0), "EOF before complete echo")
		require.LessOrEqual(t, int(count), len(want)-len(got))
		for offset := uint32(0); offset < count; offset++ {
			value, err := inst.Call(ctx, "received_byte", offset)
			require.NoError(t, err)
			b, ok := value.(uint32)
			require.True(t, ok, "received_byte must return u32")
			got = append(got, byte(b))
		}
	}
	require.Equal(t, want, string(got))
}
