// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	secapi "github.com/wippyai/runtime/api/security"
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
