// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

type commandHostRegistry struct {
	registry.Registry
	entries map[registry.ID]registry.Entry
}

func (r *commandHostRegistry) GetEntry(id registry.ID) (registry.Entry, error) {
	entry, exists := r.entries[id]
	if !exists {
		return registry.Entry{}, errors.New("entry unavailable")
	}
	return entry, nil
}

func (r *commandHostRegistry) GetAllEntries() ([]registry.Entry, error) {
	entries := make([]registry.Entry, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	return entries, nil
}

func TestCommandHostSelection(t *testing.T) {
	commandID := registry.NewID("desktop", "start")
	hostID := registry.NewID("desktop", "terminal")
	otherID := registry.NewID("installed", "terminal")
	entries := map[registry.ID]registry.Entry{
		commandID: {ID: commandID, Kind: "process.lua", Meta: map[string]any{"command": map[string]any{"name": "desktop", "host": "desktop:terminal"}}},
		hostID:    {ID: hostID, Kind: "terminal.host"},
		otherID:   {ID: otherID, Kind: "terminal.host"},
	}
	ctx := registry.WithRegistry(ctxapi.NewRootContext(), &commandHostRegistry{entries: entries})
	selected, err := resolveCommandHost(ctx, commandID)
	require.NoError(t, err)
	require.Equal(t, "desktop:terminal", selected)

	delete(entries, hostID)
	_, err = resolveCommandHost(ctx, commandID)
	require.ErrorContains(t, err, "get declared command host")
	entries[hostID] = registry.Entry{ID: hostID, Kind: "process.host"}
	_, err = resolveCommandHost(ctx, commandID)
	require.ErrorContains(t, err, "not a terminal.host")

	entries[hostID] = registry.Entry{ID: hostID, Kind: "terminal.host"}
	entries[commandID] = registry.Entry{ID: commandID, Kind: "process.lua", Meta: map[string]any{"command": map[string]any{"name": "desktop"}}}
	_, err = resolveCommandHost(ctx, commandID)
	require.ErrorContains(t, err, "multiple terminal hosts")
	delete(entries, otherID)
	selected, err = resolveCommandHost(ctx, commandID)
	require.NoError(t, err)
	require.Equal(t, "desktop:terminal", selected)
}

func TestCommandHostMetadataRejectsMalformedDeclarations(t *testing.T) {
	for _, value := range []any{nil, false, 3, "", "terminal", ":terminal", "desktop:", "desktop: terminal", "desktop:terminal\n"} {
		_, err := extractCommandMeta(map[string]any{"command": map[string]any{"name": "desktop", "host": value}})
		require.Error(t, err, "host=%v", value)
	}
	_, err := extractCommandMeta(map[string]any{"command": map[string]any{"host": "desktop:terminal"}})
	require.ErrorContains(t, err, "host requires a command name")
}
