// SPDX-License-Identifier: MPL-2.0

package indexing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"go.uber.org/zap"
)

// --- isLuaKind ---

func TestIsLuaKind(t *testing.T) {
	tests := []struct {
		kind registry.Kind
		want bool
	}{
		{luaapi.Function, true},                   // "function.lua"
		{luaapi.Process, true},                    // "process.lua"
		{registry.Kind("handler.lua"), true},      // ends with .lua
		{registry.Kind("handler.lua.test"), true}, // contains .lua.
		{registry.Kind("wasm"), false},
		{registry.Kind(""), false},
		{registry.Kind("luascript"), false}, // no suffix match
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.want, isLuaKind(tt.kind))
		})
	}
}

// --- hasErrors ---

func TestHasErrors_Empty(t *testing.T) {
	assert.False(t, hasErrors(nil))
}

func TestHasErrors_WarningOnly(t *testing.T) {
	diags := []diag.Diagnostic{
		{Severity: diag.SeverityWarning, Message: "unused variable"},
	}
	assert.False(t, hasErrors(diags))
}

func TestHasErrors_WithError(t *testing.T) {
	diags := []diag.Diagnostic{
		{Severity: diag.SeverityWarning, Message: "unused variable"},
		{Severity: diag.SeverityError, Message: "type mismatch"},
	}
	assert.True(t, hasErrors(diags))
}

func TestHasErrors_HintOnly(t *testing.T) {
	diags := []diag.Diagnostic{
		{Severity: diag.SeverityHint, Message: "suggestion"},
	}
	assert.False(t, hasErrors(diags))
}

// --- NewManagerProvider ---

func TestNewManagerProvider_Nil(t *testing.T) {
	p := NewManagerProvider(nil)
	assert.Nil(t, p)
}

// --- DocumentStore nil receiver ---

func TestDocumentStore_NilReceiver(t *testing.T) {
	var s *DocumentStore

	_, ok := s.Get(registry.NewID("app", "test"))
	assert.False(t, ok)

	// should not panic
	s.Set(registry.NewID("app", "test"), "text", 1)
	s.Delete(registry.NewID("app", "test"))
	s.Reset()
}

// --- Scheduler construction ---

func TestNewScheduler_NilLogger(t *testing.T) {
	s := NewScheduler(nil, nil, nil, 1)
	require.NotNil(t, s)
	assert.False(t, s.enabled)
}

func TestNewScheduler_ZeroWorkers(t *testing.T) {
	s := NewScheduler(zap.NewNop(), nil, nil, 0)
	require.NotNil(t, s)
	assert.True(t, s.workers >= 1)
}

func TestNewScheduler_Disabled_NilIndexer(t *testing.T) {
	s := NewScheduler(zap.NewNop(), nil, nil, 2)
	assert.False(t, s.enabled)
}

// --- Scheduler nil receiver ---

func TestScheduler_NilReceiver(t *testing.T) {
	var s *Scheduler
	// all methods should be nil-safe
	s.Start(context.Background())
	s.Stop()
	s.Enqueue([]registry.ID{registry.NewID("app", "test")})
	s.EnqueueAll()
	s.EnqueueAllSync()
	s.EnqueueSync([]registry.ID{registry.NewID("app", "test")})
	assert.NoError(t, s.WaitIdle(context.Background()))
}

// --- Scheduler disabled ---

func TestScheduler_Disabled_WaitIdle(t *testing.T) {
	s := NewScheduler(zap.NewNop(), nil, nil, 1)
	assert.NoError(t, s.WaitIdle(context.Background()))
}

func TestScheduler_Disabled_Start(t *testing.T) {
	s := NewScheduler(zap.NewNop(), nil, nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	cancel()
	s.Stop()
}

// --- Scheduler with real indexer ---

type stubProvider struct {
	deps  map[registry.ID][]registry.ID
	nodes []NodeInfo
}

func (p *stubProvider) AllNodes() []NodeInfo { return p.nodes }
func (p *stubProvider) Node(id registry.ID) (NodeInfo, error) {
	for _, n := range p.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return NodeInfo{}, nil
}
func (p *stubProvider) DirectDependencies(id registry.ID) ([]registry.ID, error) {
	return p.deps[id], nil
}
func (p *stubProvider) DependencyManifests(id registry.ID) map[string]*io.Manifest { return nil }
func (p *stubProvider) ModuleDefs() []*luaapi.ModuleDef                            { return nil }
func (p *stubProvider) BuiltinManifestHash() string                                { return "" }

func TestScheduler_EnqueueSync_EmptyIDs(t *testing.T) {
	provider := &stubProvider{}
	idx := NewIndexer(zap.NewNop(), provider, nil, nil, nil, nil, nil)
	s := NewScheduler(zap.NewNop(), idx, provider, 1)

	// empty IDs should be no-op
	s.EnqueueSync(nil)
	s.EnqueueSync([]registry.ID{})
}

func TestScheduler_WaitIdle_AlreadyIdle(t *testing.T) {
	provider := &stubProvider{}
	idx := NewIndexer(zap.NewNop(), provider, nil, nil, nil, nil, nil)
	s := NewScheduler(zap.NewNop(), idx, provider, 1)

	assert.NoError(t, s.WaitIdle(context.Background()))
}

func TestScheduler_WaitIdle_ContextCancelled(t *testing.T) {
	provider := &stubProvider{
		nodes: []NodeInfo{
			{ID: registry.NewID("app", "a"), Kind: luaapi.Function, Source: "return 1"},
		},
	}
	idx := NewIndexer(zap.NewNop(), provider, nil, nil, nil, nil, nil)
	s := NewScheduler(zap.NewNop(), idx, provider, 1)

	// enqueue without starting workers so it stays queued
	s.EnqueueSync([]registry.ID{registry.NewID("app", "a")})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.WaitIdle(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}
