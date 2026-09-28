// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
	entryutil "github.com/wippyai/runtime/system/entry"
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

func (m *mockEnvRegistry) RegisterVariable(_ envapi.Variable) error { return nil }

func (m *mockEnvRegistry) UnregisterVariable(_ registry.ID) {}

// jsonConfigTranscoder decodes an entry's raw data map into the target config the
// same way the production transcoder does, so *_env directives carried in the
// data map are seen by the central resolve pass.
type jsonConfigTranscoder struct{}

func (t *jsonConfigTranscoder) Marshal(v any) (payload.Payload, error) {
	return payload.New(v), nil
}

func (t *jsonConfigTranscoder) Unmarshal(p payload.Payload, v any) error {
	b, err := json.Marshal(p.Data())
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (t *jsonConfigTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p.Data(), format), nil
}

func cdcEntryWithData(data map[string]any) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("test", "resolve-cdc"),
		Kind: config.Postgres,
		Data: payload.New(data),
	}
}

func ctxWithEnv(reg envapi.Registry) context.Context {
	return envapi.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)
}

func TestResolveEnv_UnresolvableLeavesFieldForValidation(t *testing.T) {
	// A directive naming an unregistered variable is not applied. With no inline
	// fallback the field stays empty and the config's own validation rejects the
	// entry, rather than the resolver failing on a false "not found".
	ctx := ctxWithEnv(&mockEnvRegistry{vars: map[string]string{"DB_HOST": "h"}})
	data := map[string]any{
		"host_env":     "DB_HOST",
		"port":         5432,
		"database":     "db",
		"password":     "pw",
		"username_env": "MISSING_USER",
		"slot_name":    "slot",
		"publication":  "pub",
	}

	_, err := entryutil.DecodeEntryConfig[config.Config](ctx, &jsonConfigTranscoder{}, cdcEntryWithData(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username is required")
}

func TestResolveEnv_AppliesResolvedValues(t *testing.T) {
	ctx := ctxWithEnv(&mockEnvRegistry{vars: map[string]string{
		"H": "resolved-host", "P": "6543", "D": "resolved-db", "U": "resolved-user", "PW": "resolved-pass",
	}})
	data := map[string]any{
		"host_env":     "H",
		"port_env":     "P",
		"database_env": "D",
		"username_env": "U",
		"password_env": "PW",
		"slot_name":    "slot",
		"publication":  "pub",
	}

	cfg, err := entryutil.DecodeEntryConfig[config.Config](ctx, &jsonConfigTranscoder{}, cdcEntryWithData(data))
	require.NoError(t, err)
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
		sources: map[registry.ID]*Source{},
		infos:   map[registry.ID]config.SourceInfo{},
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

	a, ok := m.Get("test:id-a")
	require.True(t, ok)
	assert.Equal(t, "pub_a", a.Publication)
	assert.True(t, a.Streaming)

	b, ok := m.Get("test:id-b")
	require.True(t, ok)
	assert.Equal(t, []string{"public.t"}, b.Tables)

	_, ok = m.Get("unknown")
	assert.False(t, ok)

	byID, ok := m.Get(infos[0].Name)
	require.True(t, ok)
	assert.Equal(t, infos[0].Slot, byID.Slot)
}

func TestManagerStreamByID(t *testing.T) {
	m := newInspectorManager()
	id := registry.NewID("test", "id-stream")
	src := NewSource(SourceOptions{Name: id.String(), Slot: "slot_stream"})
	m.sources[id] = src
	m.storeInfo(registry.Entry{ID: id, Kind: config.Postgres}, &config.Config{SlotName: "slot_stream", Tables: []string{"public.accounts"}})

	stream, info, err := m.Stream(context.Background(), id.String(), config.StreamOptions{Buffer: 2})
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
	_, ok := m.Get(idX.String())
	assert.False(t, ok)
}

func TestManagerCollidingSlotsRemainDistinctByID(t *testing.T) {
	m := newInspectorManager()
	id1 := registry.NewID("test", "id-1")
	id2 := registry.NewID("test", "id-2")
	m.storeInfo(registry.Entry{ID: id1, Kind: config.Postgres}, &config.Config{SlotName: "shared"})
	m.storeInfo(registry.Entry{ID: id2, Kind: config.Postgres}, &config.Config{SlotName: "shared"})

	require.Len(t, m.List(), 2)

	got, ok := m.Get(id1.String())
	require.True(t, ok)
	assert.Equal(t, id1.String(), got.Name)

	got, ok = m.Get(id2.String())
	require.True(t, ok)
	assert.Equal(t, id2.String(), got.Name)
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
