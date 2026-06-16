// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
)

func newInspectorManager() *Manager {
	return &Manager{
		sources:     map[registry.ID]sourceHandle{},
		infos:       map[registry.ID]config.SourceInfo{},
		infosByName: map[string]registry.ID{},
	}
}

func TestNewManagerValidation(t *testing.T) {
	_, err := NewManager(nil, nil, nil, nil)
	assert.ErrorIs(t, err, ErrTranscoderRequired)
}

func TestManagerStoreAndList(t *testing.T) {
	m := newInspectorManager()
	idA := registry.NewID("test", "id-a")
	idB := registry.NewID("test", "id-b")

	m.storeInfo(registry.Entry{ID: idA, Kind: config.SQLite}, &config.SQLiteConfig{
		DBResource: "app:db",
		Tables:     []string{"users"},
		Snapshot:   true,
	})
	m.storeInfo(registry.Entry{ID: idB, Kind: config.SQLite}, &config.SQLiteConfig{
		DBResource: "app:db2",
	})

	infos := m.List()
	require.Len(t, infos, 2)
	names := []string{infos[0].Name, infos[1].Name}
	sort.Strings(names)
	assert.Equal(t, []string{idA.String(), idB.String()}, names)

	got, ok := m.Get(idA.String())
	require.True(t, ok)
	assert.Equal(t, "sqlite", got.Engine)
	assert.Equal(t, "app:db", got.DBResource)
	assert.Equal(t, []string{"users"}, got.Tables)
	assert.True(t, got.Snapshot)
}

func TestManagerGetMiss(t *testing.T) {
	m := newInspectorManager()
	_, ok := m.Get("missing")
	assert.False(t, ok)
}

func TestManagerRemoveInfo(t *testing.T) {
	m := newInspectorManager()
	id := registry.NewID("test", "id-a")
	m.storeInfo(registry.Entry{ID: id, Kind: config.SQLite}, &config.SQLiteConfig{DBResource: "app:db"})

	m.removeInfo(id)

	_, ok := m.Get(id.String())
	assert.False(t, ok)
	assert.Empty(t, m.List())
	assert.NotContains(t, m.infosByName, id.String())
}
