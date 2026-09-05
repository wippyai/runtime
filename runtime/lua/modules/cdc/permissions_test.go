// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
)

type sourceScope struct {
	secapi.Scope
	all bool
}

func (s *sourceScope) Evaluate(_ secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	if (s.all || resource == "test:allowed") && (action == "cdc.source" || action == "cdc.subscribe") {
		return secapi.Allow
	}
	return secapi.Deny
}

func grantTestSources(t *testing.T, l *lua.LState) {
	t.Helper()
	ctx, frame := ctxapi.OpenFrameContext(l.Context())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	l.SetContext(ctx)
	require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "cdc-test"}))
	require.NoError(t, secapi.SetScope(ctx, &sourceScope{all: true}))
}

func TestSourcePermissions(t *testing.T) {
	l := newStateWithRegistry(t, &fakeRegistry{all: []cdcapi.SourceInfo{
		{ID: registry.NewID("test", "denied")},
		{ID: registry.NewID("test", "allowed")},
	}})
	require.NoError(t, secapi.SetActor(l.Context(), secapi.Actor{ID: "reader"}))
	require.NoError(t, secapi.SetScope(l.Context(), &sourceScope{}))
	require.NoError(t, l.DoString(`
  local rows, err = cdc.list_sources()
  assert(err == nil and #rows == 1)
  local info, denied = cdc.source("test:denied")
  assert(info == nil and denied ~= nil)
  local stream, denied = cdc.stream("test:denied")
  assert(stream == nil and denied ~= nil)
  local stream, err = cdc.stream("test:allowed")
  assert(stream ~= nil and err == nil)
  stream:close()
 `))
}

func TestLazySubscriptionRechecksCallerScope(t *testing.T) {
	l := newStateWithRegistry(t, &fakeRegistry{})
	require.NoError(t, l.DoString(`pending = assert(cdc.stream("test:other"))`))
	require.NoError(t, secapi.SetScope(l.Context(), &sourceScope{}))
	require.NoError(t, l.DoString(`
  local ch, err = pending:channel()
  assert(ch == nil and err ~= nil)
  pending:close()
 `))
}
