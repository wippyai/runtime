// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"net/url"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestBuildDSNs(t *testing.T) {
	cfg := &config.Config{
		Host:     "db.internal",
		Port:     5432,
		Username: "cdc_repl",
		Password: "p@ss/word",
		Database: "appdb",
	}
	repl, admin := buildDSNs(cfg)

	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "postgres", ru.Scheme)
	assert.Equal(t, "db.internal:5432", ru.Host)
	assert.Equal(t, "/appdb", ru.Path)
	assert.Equal(t, "cdc_repl", ru.User.Username())
	pw, _ := ru.User.Password()
	assert.Equal(t, "p@ss/word", pw)
	assert.Equal(t, "database", ru.Query().Get("replication"))

	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "", au.Query().Get("replication"))
	assert.Equal(t, "db.internal:5432", au.Host)
}

func newInspectorManager() *Manager {
	return &Manager{
		sources:    map[registry.ID]*Source{},
		infos:      map[registry.ID]config.SourceInfo{},
		infosByKey: map[string]registry.ID{},
	}
}

func TestManagerStoreAndListInfos(t *testing.T) {
	m := newInspectorManager()
	m.storeInfo(registry.Entry{ID: registry.NewID("test", "id-a"), Kind: config.Postgres}, &config.Config{
		SlotName:    "slot_a",
		Publication: "pub_a",
		Streaming:   true,
	})
	m.storeInfo(registry.Entry{ID: registry.NewID("test", "id-b"), Kind: config.Postgres}, &config.Config{
		SlotName:    "slot_b",
		Tables:      []string{"public.t"},
		EventSystem: "custom_system",
	})

	infos := m.List()
	require.Len(t, infos, 2)

	slots := []string{infos[0].Slot, infos[1].Slot}
	sort.Strings(slots)
	assert.Equal(t, []string{"slot_a", "slot_b"}, slots)

	a, ok := m.Get("slot_a")
	require.True(t, ok)
	assert.Equal(t, "pub_a", a.Publication)
	assert.Equal(t, config.DefaultEventSystem, a.EventSystem)
	assert.True(t, a.Streaming)

	b, ok := m.Get("slot_b")
	require.True(t, ok)
	assert.Equal(t, "custom_system", b.EventSystem)
	assert.Equal(t, []string{"public.t"}, b.Tables)

	_, ok = m.Get("unknown")
	assert.False(t, ok)

	byID, ok := m.Get(infos[0].Name)
	require.True(t, ok)
	assert.Equal(t, infos[0].Slot, byID.Slot)
}

func TestManagerRemoveInfo(t *testing.T) {
	m := newInspectorManager()
	idX := registry.NewID("test", "id-x")
	m.storeInfo(registry.Entry{ID: idX, Kind: config.Postgres}, &config.Config{SlotName: "slot_x"})
	require.Len(t, m.List(), 1)

	m.removeInfo(idX)
	assert.Empty(t, m.List())
	_, ok := m.Get("slot_x")
	assert.False(t, ok)
}

func TestManagerCollidingSlotsDoNotLeakIndex(t *testing.T) {
	m := newInspectorManager()
	id1 := registry.NewID("test", "id-1")
	id2 := registry.NewID("test", "id-2")
	m.storeInfo(registry.Entry{ID: id1, Kind: config.Postgres}, &config.Config{SlotName: "shared"})
	m.storeInfo(registry.Entry{ID: id2, Kind: config.Postgres}, &config.Config{SlotName: "shared"})

	require.Len(t, m.List(), 2)

	m.removeInfo(id1)
	got, ok := m.Get("shared")
	require.True(t, ok)
	assert.Equal(t, id2.String(), got.Name)

	m.removeInfo(id2)
	_, ok = m.Get("shared")
	assert.False(t, ok)
}

func TestBuildDSNsCarriesOptions(t *testing.T) {
	cfg := &config.Config{
		Host:     "h",
		Port:     1,
		Username: "u",
		Password: "p",
		Database: "d",
		Options:  map[string]string{"sslmode": "require"},
	}
	repl, admin := buildDSNs(cfg)

	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "require", ru.Query().Get("sslmode"))
	assert.Equal(t, "database", ru.Query().Get("replication"))

	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "require", au.Query().Get("sslmode"))
}
