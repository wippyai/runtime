// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// mockNetworkRegistry is a minimal NetworkRegistry for tests.
type mockNetworkRegistry struct {
	known map[string]struct{}
}

func newMockNetworkRegistry(ids ...string) *mockNetworkRegistry {
	m := &mockNetworkRegistry{known: make(map[string]struct{}, len(ids))}
	for _, id := range ids {
		m.known[id] = struct{}{}
	}
	return m
}

func (m *mockNetworkRegistry) GetNetwork(id registry.ID) (netapi.Service, error) {
	if _, ok := m.known[id.String()]; !ok {
		return nil, netapi.ErrNetworkNotFound
	}
	return nil, nil
}

func (m *mockNetworkRegistry) HasNetwork(id registry.ID) bool {
	_, ok := m.known[id.String()]
	return ok
}

func (m *mockNetworkRegistry) NetworkKind(_ registry.ID) registry.Kind {
	return netapi.KindSOCKS5
}

// ctxWithNetworkRegistry returns a context carrying the given NetworkRegistry
// on a fresh AppContext.
func ctxWithNetworkRegistry(reg netapi.NetworkRegistry) context.Context {
	ctx := ctxapi.NewRootContext()
	if reg != nil {
		ctx = netapi.WithNetworkRegistry(ctx, reg)
	}
	return ctx
}

// findNetworkPair returns the string value associated with the network key
// in a []ctxapi.Pair, or empty string if not present.
func findNetworkPair(pairs []ctxapi.Pair, expected string) bool {
	pair := netapi.DefaultNetworkPair(expected)
	for _, p := range pairs {
		if p.Key == pair.Key && p.Value == pair.Value {
			return true
		}
	}
	return false
}

// --- Tests ---

func TestManager_Start_NoNetworkOption_Passthrough(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	start := &process.Start{
		HostID:  "test-host",
		Source:  registry.NewID("test", "source"),
		Options: attrs.NewBag(),
	}
	_, err := mgr.Start(ctxWithNetworkRegistry(nil), start)
	require.NoError(t, err)

	assert.Empty(t, start.Context, "no network option → no pair should be appended")
	assert.True(t, host.runCalled)
}

func TestManager_Start_NetworkOption_InjectsDefaultNetworkPair(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	reg := newMockNetworkRegistry("app.net:socks5")
	opts := attrs.NewBag()
	opts.Set(netapi.OptionKeyNetwork, "app.net:socks5")

	start := &process.Start{
		HostID:  "test-host",
		Source:  registry.NewID("test", "source"),
		Options: opts,
	}

	_, err := mgr.Start(ctxWithNetworkRegistry(reg), start)
	require.NoError(t, err)

	assert.True(t, findNetworkPair(start.Context, "app.net:socks5"),
		"network option should inject DefaultNetworkPair into start.Context")
	assert.True(t, host.runCalled)
}

func TestManager_Start_NetworkOption_UnknownID_ReturnsErrNetworkNotFound(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	reg := newMockNetworkRegistry("app.net:other")
	opts := attrs.NewBag()
	opts.Set(netapi.OptionKeyNetwork, "app.net:does-not-exist")

	start := &process.Start{
		HostID:  "test-host",
		Source:  registry.NewID("test", "source"),
		Options: opts,
	}

	_, err := mgr.Start(ctxWithNetworkRegistry(reg), start)
	require.Error(t, err)
	assert.True(t, errors.Is(err, netapi.ErrNetworkNotFound))
	assert.False(t, host.runCalled, "host.Run must not be called when network lookup fails")
}

func TestManager_Start_NetworkOption_NoRegistry_ReturnsErrNetworkNotFound(t *testing.T) {
	// When the option is set but no NetworkRegistry is available on the
	// context, the lookup must reject the start — silently dropping the
	// network selection would route traffic through the clearnet.
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	opts := attrs.NewBag()
	opts.Set(netapi.OptionKeyNetwork, "app.net:socks5")

	start := &process.Start{
		HostID:  "test-host",
		Source:  registry.NewID("test", "source"),
		Options: opts,
	}

	_, err := mgr.Start(ctxWithNetworkRegistry(nil), start)
	require.Error(t, err)
	assert.True(t, errors.Is(err, netapi.ErrNetworkNotFound))
	assert.False(t, host.runCalled, "host.Run must not be called when registry is unavailable")
}

func TestManager_Start_NetworkOption_AppendsToExistingContext(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	reg := newMockNetworkRegistry("app.net:socks5")
	opts := attrs.NewBag()
	opts.Set(netapi.OptionKeyNetwork, "app.net:socks5")

	preexistingKey := &ctxapi.Key{Name: "preexisting.test"}
	preexisting := ctxapi.Pair{Key: preexistingKey, Value: "keep-me"}

	start := &process.Start{
		HostID:  "test-host",
		Source:  registry.NewID("test", "source"),
		Options: opts,
		Context: []ctxapi.Pair{preexisting},
	}

	_, err := mgr.Start(ctxWithNetworkRegistry(reg), start)
	require.NoError(t, err)

	require.Len(t, start.Context, 2, "network pair should be appended, not replace existing pairs")
	assert.Equal(t, preexistingKey, start.Context[0].Key)
	assert.Equal(t, "keep-me", start.Context[0].Value)
	assert.True(t, findNetworkPair(start.Context, "app.net:socks5"))
}

// TestManager_Start_NetworkOption_EmptyStringIgnored verifies that setting the
// option to "" is treated as "no override" — the same as not setting it.
// This matters because callers sometimes default-initialize option bags.
func TestManager_Start_NetworkOption_EmptyStringIgnored(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	opts := attrs.NewBag()
	opts.Set(netapi.OptionKeyNetwork, "")

	start := &process.Start{
		HostID:  "test-host",
		Source:  registry.NewID("test", "source"),
		Options: opts,
	}

	// No registry attached — if empty string triggered lookup this would fail.
	_, err := mgr.Start(ctxWithNetworkRegistry(nil), start)
	require.NoError(t, err)
	assert.Empty(t, start.Context)
	assert.True(t, host.runCalled)
}
