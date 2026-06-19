// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"net/url"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
)

type mockEnvRegistry struct {
	vars map[string]string
}

func (m *mockEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if v, ok := m.vars[name]; ok {
		return v, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *mockEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	v, ok := m.vars[name]
	return v, ok, nil
}

func (m *mockEnvRegistry) Set(_ context.Context, name, value string) error {
	m.vars[name] = value
	return nil
}

func (m *mockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	return m.vars, nil
}

func (m *mockEnvRegistry) GetStorage(_ context.Context, _ registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}

func (m *mockEnvRegistry) RegisterStorage(_ registry.ID, _ envapi.Storage) {}

func TestResolveEnv_FailsFastOnUnresolvable(t *testing.T) {
	m := &Manager{env: &mockEnvRegistry{vars: map[string]string{"DB_HOST": "h"}}}
	cfg := &config.Config{HostEnv: "DB_HOST", UsernameEnv: "MISSING_USER"}

	err := m.resolveEnv(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be resolved")
	assert.Contains(t, err.Error(), "environment variable not found")
}

func TestResolveEnv_AppliesResolvedValues(t *testing.T) {
	m := &Manager{env: &mockEnvRegistry{vars: map[string]string{
		"H": "resolved-host", "P": "6543", "D": "resolved-db", "U": "resolved-user", "PW": "resolved-pass",
	}}}
	cfg := &config.Config{HostEnv: "H", PortEnv: "P", DatabaseEnv: "D", UsernameEnv: "U", PasswordEnv: "PW"}

	require.NoError(t, m.resolveEnv(context.Background(), cfg))
	assert.Equal(t, "resolved-host", cfg.Host)
	assert.Equal(t, 6543, cfg.Port)
	assert.Equal(t, "resolved-db", cfg.Database)
	assert.Equal(t, "resolved-user", cfg.Username)
	assert.Equal(t, "resolved-pass", cfg.Password)
}

func TestBuildDSNs_RejectsEmptyRequiredField(t *testing.T) {
	base := func() *config.Config {
		return &config.Config{Host: "h", Port: 5432, Username: "u", Database: "d"}
	}

	c := base()
	c.Host = ""
	_, _, err := buildDSNs(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host")

	c = base()
	c.Port = 0
	_, _, err = buildDSNs(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port")

	c = base()
	c.Username = ""
	_, _, err = buildDSNs(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username")

	c = base()
	c.Database = ""
	_, _, err = buildDSNs(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database")
}

func TestBuildDSNs(t *testing.T) {
	cfg := &config.Config{
		Host:     "db.internal",
		Port:     5432,
		Username: "cdc_repl",
		Password: "p@ss/word",
		Database: "appdb",
	}
	repl, admin, err := buildDSNs(cfg)
	require.NoError(t, err)

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
		SlotName: "slot_b",
		Tables:   []string{"public.t"},
	})

	infos := m.List()
	require.Len(t, infos, 2)

	slots := []string{infos[0].Slot, infos[1].Slot}
	sort.Strings(slots)
	assert.Equal(t, []string{"slot_a", "slot_b"}, slots)

	a, ok := m.Get("slot_a")
	require.True(t, ok)
	assert.Equal(t, "pub_a", a.Publication)
	assert.True(t, a.Streaming)

	b, ok := m.Get("slot_b")
	require.True(t, ok)
	assert.Equal(t, []string{"public.t"}, b.Tables)

	_, ok = m.Get("unknown")
	assert.False(t, ok)

	byID, ok := m.Get(infos[0].Name)
	require.True(t, ok)
	assert.Equal(t, infos[0].Slot, byID.Slot)
}

func TestManagerStreamBySlotAndID(t *testing.T) {
	m := newInspectorManager()
	id := registry.NewID("test", "id-stream")
	src := NewSource(SourceOptions{Name: id.String(), Slot: "slot_stream"})
	m.sources[id] = src
	m.storeInfo(registry.Entry{ID: id, Kind: config.Postgres}, &config.Config{SlotName: "slot_stream", Tables: []string{"public.accounts"}})

	stream, info, err := m.Stream(context.Background(), "slot_stream", config.StreamOptions{Buffer: 2})
	require.NoError(t, err)
	require.NotNil(t, stream)
	assert.Equal(t, "slot_stream", info.Slot)
	stream.Close()

	stream, info, err = m.Stream(context.Background(), id.String(), config.StreamOptions{})
	require.NoError(t, err)
	require.NotNil(t, stream)
	assert.Equal(t, id.String(), info.Name)
	stream.Close()
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
	repl, admin, err := buildDSNs(cfg)
	require.NoError(t, err)

	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "require", ru.Query().Get("sslmode"))
	assert.Equal(t, "database", ru.Query().Get("replication"))

	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "require", au.Query().Get("sslmode"))
}
