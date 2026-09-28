// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

func TestSecureService_ImplementsInterface(t *testing.T) {
	var _ netapi.Service = (*SecureService)(nil)
}

// nonStrictCtx returns a context with AppContext and strict mode disabled,
// so operations with no actor/scope are allowed.
func nonStrictCtx() context.Context {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	return ctx
}

// strictCtx returns a context with AppContext and strict mode enabled,
// so operations with no actor/scope are denied.
func strictCtx() context.Context {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, true)
	return ctx
}

func TestSecureService_DialContext_Denied(t *testing.T) {
	svc := NewSecureService()

	conn, err := svc.DialContext(strictCtx(), "tcp", "example.com:80")
	assert.Nil(t, conn)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)
}

func TestSecureService_Listen_Denied(t *testing.T) {
	svc := NewSecureService()

	ln, err := svc.Listen(strictCtx(), "tcp", "127.0.0.1:0")
	assert.Nil(t, ln)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)
}

func TestSecureService_LookupHost_Denied(t *testing.T) {
	svc := NewSecureService()

	addrs, err := svc.LookupHost(strictCtx(), "example.com")
	assert.Nil(t, addrs)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)
}

func TestSecureService_Listen_Allowed(t *testing.T) {
	svc := NewSecureService()

	ln, err := svc.Listen(nonStrictCtx(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NotNil(t, ln)
	_ = ln.Close()
}

func TestSecureService_DialContext_Allowed(t *testing.T) {
	svc := NewSecureService()

	// Start a listener to connect to
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	conn, err := svc.DialContext(nonStrictCtx(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
}

func TestSecureService_LookupHost_Allowed(t *testing.T) {
	svc := NewSecureService()

	addrs, err := svc.LookupHost(nonStrictCtx(), "localhost")
	require.NoError(t, err)
	assert.NotEmpty(t, addrs)
}

func TestSecureService_ListenPacket_Denied(t *testing.T) {
	svc := NewSecureService()

	conn, err := svc.ListenPacket(strictCtx(), "udp", "127.0.0.1:0")
	assert.Nil(t, conn)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)
}

func TestSecureService_ListenPacket_Allowed(t *testing.T) {
	svc := NewSecureService()

	conn, err := svc.ListenPacket(nonStrictCtx(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
}

func TestSecureService_DialContext_PrivateIP(t *testing.T) {
	svc := NewSecureService()

	// Private IP check uses "tcp://" prefix which doesn't parse as valid URL
	// with a host, so private IP check only fires for real URLs.
	// The security.IsAllowed check fires first in strict mode.
	conn, err := svc.DialContext(strictCtx(), "tcp", "127.0.0.1:80")
	assert.Nil(t, conn)
	require.Error(t, err)
}

func TestSecureService_DialContext_UsesSelectedNetwork(t *testing.T) {
	overlay := &mockService{}
	reg := NewRegistry(zap.NewNop())
	reg.Register(registry.ParseID("app.net:test"), overlay, netapi.KindSOCKS5)

	ctx := nonStrictCtx()
	ctx = netapi.WithNetworkRegistry(ctx, reg)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:test")))

	conn, err := NewSecureService().DialContext(ctx, "tcp", "example.invalid:443")
	require.NoError(t, err)
	assert.Nil(t, conn)
	assert.True(t, overlay.dialCalled)
}

func TestSecureService_DialContext_DoesNotFallbackFromSelectedNetwork(t *testing.T) {
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	ctx := nonStrictCtx()
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:missing")))

	conn, err := NewSecureService().DialContext(ctx, "tcp", listener.Addr().String())
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err)
}

type testPolicy struct {
	allowed map[string]bool
	id      registry.ID
}

func newTestPolicy() *testPolicy {
	return &testPolicy{
		id:      registry.NewID("policy", "test"),
		allowed: make(map[string]bool),
	}
}

func (p *testPolicy) ID() registry.ID {
	return p.id
}

func (p *testPolicy) Evaluate(_ secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	if p.allowed[action] || p.allowed[action+":"+resource] {
		return secapi.Allow
	}
	return secapi.Deny
}

func testSecurityContext(t *testing.T, policy *testPolicy) context.Context {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, true)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "test-actor"}))
	require.NoError(t, secapi.SetScope(ctx, secsystem.NewScope([]secapi.Policy{policy})))
	return ctx
}

func TestSecureService_DialContext_Rebinding(t *testing.T) {
	svc := NewSecureService()

	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	policy := newTestPolicy()
	policy.allowed["socket.connect"] = true
	ctx := testSecurityContext(t, policy)

	conn, err := svc.DialContext(ctx, "tcp", "localhost:"+port)
	require.Nil(t, conn)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)

	ln.Close()
	select {
	case c := <-accepted:
		_ = c.Close()
		t.Fatal("listener accepted connection")
	default:
	}
}

func TestSecureService_DialContext_FailClosed(t *testing.T) {
	svc := NewSecureService()

	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	policyDenied := newTestPolicy()
	policyDenied.allowed["socket.connect"] = true
	ctxDenied := testSecurityContext(t, policyDenied)

	conn, err := svc.DialContext(ctxDenied, "tcp", ln.Addr().String())
	require.Nil(t, conn)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)

	policyAllowed := newTestPolicy()
	policyAllowed.allowed["socket.connect"] = true
	policyAllowed.allowed["socket.private_ip"] = true
	ctxAllowed := testSecurityContext(t, policyAllowed)

	conn, err = svc.DialContext(ctxAllowed, "tcp", ln.Addr().String())
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
}

func TestSecureService_DialContext_ResolutionFailure(t *testing.T) {
	svc := NewSecureService()

	policy := newTestPolicy()
	policy.allowed["socket.connect"] = true
	ctx := testSecurityContext(t, policy)

	conn, err := svc.DialContext(ctx, "tcp", "nonexistent.invalid:80")
	require.Nil(t, conn)
	require.Error(t, err)
	require.False(t, errors.Is(err, netapi.ErrAccessDenied))
}

func TestSecureService_DialContext_PublicLiteral(t *testing.T) {
	svc := NewSecureService()

	policy := newTestPolicy()
	policy.allowed["socket.connect"] = true
	ctx := testSecurityContext(t, policy)

	ctxCanceled, cancel := context.WithCancel(ctx)
	cancel()

	conn, err := svc.DialContext(ctxCanceled, "tcp", "192.0.2.1:9")
	require.Nil(t, conn)
	require.Error(t, err)
	require.False(t, errors.Is(err, netapi.ErrAccessDenied))
}

func TestSecureService_DialContext_ErrorsIsAccessDenied(t *testing.T) {
	svc := NewSecureService()

	policy := newTestPolicy()
	policy.allowed["socket.connect"] = true
	ctx := testSecurityContext(t, policy)

	conn, err := svc.DialContext(ctx, "tcp", "127.0.0.1:80")
	require.Nil(t, conn)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)
}

func TestSecureService_MissingNetworkRegistry_ApiError(t *testing.T) {
	ctx := nonStrictCtx()
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:missing")))

	svc := NewSecureService()

	conn, err := svc.DialContext(ctx, "tcp", "127.0.0.1:80")
	require.Nil(t, conn)
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Invalid, apiErr.Kind())

	ln, err := svc.ListenPacket(ctx, "udp", "127.0.0.1:0")
	require.Nil(t, ln)
	require.Error(t, err)
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Invalid, apiErr.Kind())

	addrs, err := svc.LookupHost(ctx, "localhost")
	require.Nil(t, addrs)
	require.Error(t, err)
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Invalid, apiErr.Kind())
}

func TestSecureService_NetworkLookupError_ApiError(t *testing.T) {
	overlay := &mockService{}
	reg := NewRegistry(zap.NewNop())
	reg.Register(registry.ParseID("app.net:test"), overlay, netapi.KindSOCKS5)

	ctx := nonStrictCtx()
	ctx = netapi.WithNetworkRegistry(ctx, reg)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:notfound")))

	svc := NewSecureService()

	conn, err := svc.DialContext(ctx, "tcp", "127.0.0.1:80")
	require.Nil(t, conn)
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Internal, apiErr.Kind())
	assert.Contains(t, apiErr.Error(), "lookup failed")
	require.ErrorIs(t, err, netapi.ErrNetworkNotFound)
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		addr    string
		private bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"169.254.169.254", true},
		{"::1", true},
		{"fe80::1%lo", true},
		{"::ffff:127.0.0.1", true},
		{"0.0.0.0", true},
		{"fc00::1", true},
		{"192.0.2.1", false},
		{"2001:db8::1", false},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			addr, err := netip.ParseAddr(tc.addr)
			require.NoError(t, err)
			assert.Equal(t, tc.private, isPrivateIP(addr))
		})
	}
}

func TestControlHook(t *testing.T) {
	policyDenied := newTestPolicy()
	policyDenied.allowed["socket.connect"] = true
	ctxDenied := testSecurityContext(t, policyDenied)

	err := controlHook(ctxDenied, "tcp", "[fe80::1%lo]:80", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)

	err = controlHook(ctxDenied, "tcp", "[::ffff:127.0.0.1]:80", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)

	err = controlHook(ctxDenied, "tcp", "garbage", nil)
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Invalid, apiErr.Kind())

	err = controlHook(ctxDenied, "unix", "/run/x.sock", nil)
	require.NoError(t, err)
}

func TestSecureService_DialContext_UnixSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping unix socket test on windows")
	}

	socketPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := new(net.ListenConfig).Listen(context.Background(), "unix", socketPath)
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	svc := NewSecureService()
	policy := newTestPolicy()
	policy.allowed["socket.connect"] = true
	ctx := testSecurityContext(t, policy)

	conn, err := svc.DialContext(ctx, "unix", socketPath)
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
}

func TestSecureService_DialContext_Overlay(t *testing.T) {
	overlay := &mockService{}
	reg := NewRegistry(zap.NewNop())
	reg.Register(registry.ParseID("app.net:test"), overlay, netapi.KindSOCKS5)

	ctxBase := nonStrictCtx()
	ctxBase = netapi.WithNetworkRegistry(ctxBase, reg)
	ctx, frame := ctxapi.OpenFrameContext(ctxBase)
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:test")))

	svc := NewSecureService()

	// 10.0.0.5 without port is refused with an Invalid-kind error
	conn, err := svc.DialContext(ctx, "tcp", "10.0.0.5")
	require.Nil(t, conn)
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Invalid, apiErr.Kind())

	// 10.0.0.5:80 with policy denied is ErrAccessDenied
	policyDenied := newTestPolicy()
	policyDenied.allowed["socket.connect"] = true
	ctxPolicyDenied := testSecurityContext(t, policyDenied)
	ctxPolicyDenied = netapi.WithNetworkRegistry(ctxPolicyDenied, reg)
	ctxPolicyDenied, frameDenied := ctxapi.OpenFrameContext(ctxPolicyDenied)
	require.NoError(t, frameDenied.SetMultiple(netapi.DefaultNetworkPair("app.net:test")))

	conn, err = svc.DialContext(ctxPolicyDenied, "tcp", "10.0.0.5:80")
	require.Nil(t, conn)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)

	// example.invalid:80 reaches the overlay service
	conn, err = svc.DialContext(ctx, "tcp", "example.invalid:80")
	require.NoError(t, err)
	assert.Nil(t, conn)
	assert.True(t, overlay.dialCalled)
}
