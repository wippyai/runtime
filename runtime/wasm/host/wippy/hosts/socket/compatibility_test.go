// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDeprecationMetadata(t *testing.T) {
	assert.True(t, LegacyDeprecation.Deprecated)
	assert.Equal(t, Namespace, LegacyDeprecation.Replacement)
	assert.Equal(t, MinimumDeprecationVersions, LegacyDeprecation.MinimumVersions)
	assert.Equal(t, 10, LegacyDeprecation.MinimumVersions)
	assert.Empty(t, LegacyDeprecation.FirstReleasedVersion)
	assert.NotEmpty(t, LegacyDeprecation.Notes)

	assert.Equal(t, "wippy:runtime/socket@0.1.0", Namespace)
	assert.Equal(t, "wippy:sock/tcp", LegacyNamespace)
	assert.Equal(t, "wippy:sock", LegacyYAMLName)
	assert.Equal(t, "socket", CanonicalProfileName)
}

func oldSocketTestWAT(port int) string {
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
)`, LegacyNamespace, LegacyNamespace, LegacyNamespace, LegacyNamespace, port)
}

func canonicalSocketTestWAT(port int) string {
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
)`, Namespace, Namespace, Namespace, Namespace, port)
}

func mixedSocketTestWAT(port int) string {
	return fmt.Sprintf(`(module
  (import %q "connect" (func $old_connect (param i32 i32 i32 i32) (result i64)))
  (import %q "close" (func $old_close (param i32) (result i32)))
  (import %q "connect" (func $new_connect (param i32 i32 i32 i32) (result i64)))
  (import %q "send" (func $new_send (param i32 i32 i32) (result i64)))
  (import %q "recv" (func $new_recv (param i32 i32 i32) (result i64)))
  (import %q "close" (func $new_close (param i32) (result i32)))
  (memory (export "memory") 1)
  (func (export "received_byte") (param i32) (result i32)
    local.get 0 i32.const 64 i32.add i32.load8_u)
  (data (i32.const 0) "127.0.0.1")
  (data (i32.const 32) "ping")
  (func (export "old_connect") (result i64)
    i32.const 0 i32.const 9 i32.const %d i32.const 1000 call $old_connect)
  (func (export "new_connect") (result i64)
    i32.const 0 i32.const 9 i32.const %d i32.const 1000 call $new_connect)
  (func (export "new_send") (param $handle i32) (result i64)
    local.get $handle i32.const 32 i32.const 4 call $new_send)
  (func (export "new_recv") (param $handle i32) (result i64)
    local.get $handle i32.const 64 i32.const 16 call $new_recv)
  (func (export "old_close") (param $handle i32) (result i32)
    local.get $handle call $old_close)
  (func (export "new_close") (param $handle i32) (result i32)
    local.get $handle call $new_close)
)`, LegacyNamespace, LegacyNamespace, Namespace, Namespace, Namespace, Namespace, port, port)
}

func startEchoPeer(t *testing.T, listener net.Listener) func() {
	t.Helper()
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
				buf := make([]byte, 128)
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

	return func() {
		cancel()
		_ = listener.Close()
		wg.Wait()
		_ = ctx
	}
}

// Tests that old and new WAT clients send/recv/close with the same loopback peer using ONE runtime,
// and verifies that the deprecation warning is emitted exactly once for legacy binary usage
// and zero times for canonical binary usage.
func TestOldAndNewWATClients_SameLoopbackPeer_SingleRuntime(t *testing.T) {
	ctx := socketTestContext()
	listener, port := listenForSocketTest(t)
	stopPeer := startEchoPeer(t, listener)
	defer stopPeer()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	core, observedLogs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	require.NoError(t, Register(rt, WithLogger(logger)))

	// Compile old WAT module
	oldMod, err := rt.LoadWAT(ctx, oldSocketTestWAT(port), socketTestWIT+"received_byte: func(offset: u32) -> u32;\n")
	require.NoError(t, err)
	require.NoError(t, oldMod.Compile(ctx))

	// Compile canonical WAT module
	newMod, err := rt.LoadWAT(ctx, canonicalSocketTestWAT(port), socketTestWIT+"received_byte: func(offset: u32) -> u32;\n")
	require.NoError(t, err)
	require.NoError(t, newMod.Compile(ctx))

	// Instantiate and execute old WAT client
	oldInst, err := oldMod.Instantiate(ctx)
	require.NoError(t, err)
	defer oldInst.Close(ctx)

	status, handle := callPacked(ctx, t, oldInst, "connect")
	require.Equal(t, StatusOK, status)
	require.NotZero(t, handle)

	// Verify deprecation warning logged exactly once on old connect
	warnLogs := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogs, 1, "warning must be emitted exactly once for legacy binary namespace")
	assert.Contains(t, warnLogs[0].Message, "deprecated socket binary namespace used")
	fields := warnLogs[0].ContextMap()
	assert.Equal(t, LegacyNamespace, fields["namespace"])
	assert.Equal(t, Namespace, fields["replacement"])
	assert.Equal(t, int64(10), fields["minimum_versions"])

	// Send/recv/close on old client
	status, written := callPacked(ctx, t, oldInst, "send", handle)
	require.Equal(t, StatusOK, status)
	require.Equal(t, uint32(4), written)

	requireEchoPayload(ctx, t, oldInst, "recv", handle, "ping")

	closeStatus := callStatus(ctx, t, oldInst, "close", handle)
	require.Equal(t, StatusOK, closeStatus)

	// Verify no additional warnings logged during send/recv/close
	warnLogsAfterOps := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogsAfterOps, 1, "legacy binary functions must dedup and avoid per-message log spam")

	// Instantiate and execute canonical WAT client
	newInst, err := newMod.Instantiate(ctx)
	require.NoError(t, err)
	defer newInst.Close(ctx)

	status, handleNew := callPacked(ctx, t, newInst, "connect")
	require.Equal(t, StatusOK, status)
	require.NotZero(t, handleNew)

	status, written = callPacked(ctx, t, newInst, "send", handleNew)
	require.Equal(t, StatusOK, status)
	require.Equal(t, uint32(4), written)

	requireEchoPayload(ctx, t, newInst, "recv", handleNew, "ping")

	closeStatus = callStatus(ctx, t, newInst, "close", handleNew)
	require.Equal(t, StatusOK, closeStatus)

	// Verify zero additional warnings for canonical client
	warnLogsAfterCanonical := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnLogsAfterCanonical, 1, "canonical usage must emit no warnings")
}

// Tests that mixed alias handles in the same instance operate on the same connection,
// cannot bypass per-instance limits, and cannot cross instance boundaries.
func TestMixedAliasHandles_BoundsAndSecurity(t *testing.T) {
	listener, port := listenForSocketTest(t)
	stopPeer := startEchoPeer(t, listener)
	defer stopPeer()

	base := socketTestContext()
	ctx := wippyhost.WithCallLimits(base, wasmapi.LimitsConfig{MaxOpenSockets: 1})

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	require.NoError(t, Register(rt))

	mixedMod, err := rt.LoadWAT(ctx, mixedSocketTestWAT(port), `
old_connect: func() -> u64;
new_connect: func() -> u64;
new_send: func(handle: u32) -> u64;
new_recv: func(handle: u32) -> u64;
old_close: func(handle: u32) -> u32;
new_close: func(handle: u32) -> u32;
received_byte: func(offset: u32) -> u32;
`)
	require.NoError(t, err)
	require.NoError(t, mixedMod.Compile(ctx))

	inst1, err := mixedMod.Instantiate(ctx)
	require.NoError(t, err)
	defer inst1.Close(ctx)

	// 1. Connect via old namespace
	status, handle := callPacked(ctx, t, inst1, "old_connect")
	require.Equal(t, StatusOK, status)
	require.NotZero(t, handle)

	// 2. Attempt to connect via new namespace when limit is 1: must be rejected with StatusLimit!
	// (Mixed alias cannot bypass instance socket bounds)
	status, _ = callPacked(ctx, t, inst1, "new_connect")
	require.Equal(t, StatusLimit, status, "second connect via canonical namespace must fail under instance limit")

	// 3. Send and Recv on the old handle using the new namespace functions (shared handle table)
	status, written := callPacked(ctx, t, inst1, "new_send", handle)
	require.Equal(t, StatusOK, status)
	require.Equal(t, uint32(4), written)

	requireEchoPayload(ctx, t, inst1, "new_recv", handle, "ping")

	// 4. Instance 2 cannot access inst1's handle even with new close (instance isolation)
	inst2, err := mixedMod.Instantiate(ctx)
	require.NoError(t, err)
	defer inst2.Close(ctx)

	statusForeign := callStatus(ctx, t, inst2, "new_close", handle)
	require.Equal(t, StatusUnknownHandle, statusForeign, "foreign instance cannot access handle")

	// 5. Close via old namespace in inst1
	statusClose := callStatus(ctx, t, inst1, "old_close", handle)
	require.Equal(t, StatusOK, statusClose)

	// 6. After close, limit capacity is released for canonical connect
	statusNew, handle2 := callPacked(ctx, t, inst1, "new_connect")
	require.Equal(t, StatusOK, statusNew)
	require.NotZero(t, handle2)

	_ = callStatus(ctx, t, inst1, "new_close", handle2)
}

// Tests that instance close properly releases connections opened via legacy namespace.
func TestLegacyInstanceCloseCleansUpConnections(t *testing.T) {
	ctx := socketTestContext()
	listener, port := listenForSocketTest(t)
	defer listener.Close()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	require.NoError(t, Register(rt))

	mod, err := rt.LoadWAT(ctx, oldSocketTestWAT(port), socketTestWIT+"received_byte: func(offset: u32) -> u32;\n")
	require.NoError(t, err)
	require.NoError(t, mod.Compile(ctx))

	inst, err := mod.Instantiate(ctx)
	require.NoError(t, err)

	status, _ := callPacked(ctx, t, inst, "connect")
	require.Equal(t, StatusOK, status)

	peer, err := listener.Accept()
	require.NoError(t, err)
	defer peer.Close()

	require.NoError(t, inst.Close(ctx))

	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	n, err := peer.Read(buf)
	assert.Zero(t, n)
	assert.ErrorIs(t, err, io.EOF)
}

// TCP may split an echo across reads. Check guest-visible bytes, not only counts.
func requireEchoPayload(ctx context.Context, t *testing.T, inst *wasmrt.Instance, method string, handle uint32, want string) {
	t.Helper()
	got := make([]byte, 0, len(want))
	for len(got) < len(want) {
		status, count := callPacked(ctx, t, inst, method, handle)
		require.Equal(t, StatusOK, status)
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
