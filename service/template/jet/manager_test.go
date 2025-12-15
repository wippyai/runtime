package jet

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/template"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

type mockBus struct {
	mu     sync.Mutex
	events []event.Event
}

func (m *mockBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func (m *mockBus) Send(_ context.Context, e event.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockBus) getEvents() []event.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

func newTestManager(_ *testing.T) (*Manager, *mockBus) {
	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)
	bus := &mockBus{}
	log := zap.NewNop()
	m := NewManager(bus, transcoder, log)
	return m, bus
}

func makeSetEntry(id registry.ID, cfg *template.SetConfig) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: template.Set,
		Data: payload.New(map[string]any{
			"engine": map[string]any{
				"development_mode": cfg.Engine.DevelopmentMode,
				"delimiters": map[string]any{
					"left":  cfg.Engine.Delimiters.Left,
					"right": cfg.Engine.Delimiters.Right,
				},
			},
		}),
	}
}

func makeTemplateEntry(id registry.ID, cfg *template.Config) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: template.Jet,
		Data: payload.New(map[string]any{
			"source": cfg.Source,
			"set": map[string]any{
				"ns":   cfg.Set.NS,
				"name": cfg.Set.Name,
			},
		}),
	}
}

func TestNewManager(t *testing.T) {
	m, _ := newTestManager(t)
	require.NotNil(t, m)
	assert.NotNil(t, m.sets)
	assert.NotNil(t, m.templates)
}

func TestManager_AddSet(t *testing.T) {
	m, bus := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
			Delimiters: template.DelimiterConfig{
				Left:  "{{",
				Right: "}}",
			},
		},
	}

	entry := makeSetEntry(setID, cfg)
	err := m.Add(ctx, entry)
	require.NoError(t, err)

	set, err := m.GetTemplateSet(setID)
	require.NoError(t, err)
	require.NotNil(t, set)
	assert.Equal(t, setID, set.ID())

	events := bus.getEvents()
	require.Len(t, events, 1)
	assert.Equal(t, resource.System, events[0].System)
	assert.Equal(t, resource.Register, events[0].Kind)
}

func TestManager_AddSetAlreadyExists(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
		},
	}

	entry := makeSetEntry(setID, cfg)
	err := m.Add(ctx, entry)
	require.NoError(t, err)

	err = m.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_AddTemplate(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
		},
	}
	setEntry := makeSetEntry(setID, setCfg)
	err := m.Add(ctx, setEntry)
	require.NoError(t, err)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello {{name}}",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	err = m.Add(ctx, tplEntry)
	require.NoError(t, err)

	set, _ := m.GetTemplateSet(setID)
	source, err := set.GetTemplateSource("my-template")
	require.NoError(t, err)
	assert.Equal(t, "Hello {{name}}", source)
}

func TestManager_AddTemplateWithMeta(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	err := m.Add(ctx, setEntry)
	require.NoError(t, err)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	tplEntry.Meta = attrs.NewBagFrom(map[string]any{"name": "custom-name"})
	err = m.Add(ctx, tplEntry)
	require.NoError(t, err)

	set, _ := m.GetTemplateSet(setID)
	source, err := set.GetTemplateSource("custom-name")
	require.NoError(t, err)
	assert.Equal(t, "Hello", source)
}

func TestManager_AddTemplateSetNotFound(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    registry.NewID("test", "missing-set"),
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	err := m.Add(ctx, tplEntry)
	require.Error(t, err)
	assert.ErrorIs(t, err, template.ErrSetNotFound)
}

func TestManager_UpdateTemplate(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	_ = m.Add(ctx, tplEntry)

	updatedCfg := &template.Config{
		Source: "Updated Hello",
		Set:    setID,
	}
	updatedEntry := makeTemplateEntry(tplID, updatedCfg)
	err := m.Update(ctx, updatedEntry)
	require.NoError(t, err)

	set, _ := m.GetTemplateSet(setID)
	source, _ := set.GetTemplateSource("my-template")
	assert.Equal(t, "Updated Hello", source)
}

func TestManager_UpdateTemplateRename(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	_ = m.Add(ctx, tplEntry)

	updatedCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	updatedEntry := makeTemplateEntry(tplID, updatedCfg)
	updatedEntry.Meta = attrs.NewBagFrom(map[string]any{"name": "new-name"})
	err := m.Update(ctx, updatedEntry)
	require.NoError(t, err)

	set, _ := m.GetTemplateSet(setID)
	_, err = set.GetTemplateSource("my-template")
	assert.ErrorIs(t, err, template.ErrTemplateNotFound)

	source, err := set.GetTemplateSource("new-name")
	require.NoError(t, err)
	assert.Equal(t, "Hello", source)
}

func TestManager_UpdateTemplateMoveSet(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID1 := registry.NewID("test", "set-1")
	setID2 := registry.NewID("test", "set-2")

	setCfg := &template.SetConfig{}
	_ = m.Add(ctx, makeSetEntry(setID1, setCfg))
	_ = m.Add(ctx, makeSetEntry(setID2, setCfg))

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID1,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	_ = m.Add(ctx, tplEntry)

	updatedCfg := &template.Config{
		Source: "Hello",
		Set:    setID2,
	}
	updatedEntry := makeTemplateEntry(tplID, updatedCfg)
	err := m.Update(ctx, updatedEntry)
	require.NoError(t, err)

	set1, _ := m.GetTemplateSet(setID1)
	_, err = set1.GetTemplateSource("my-template")
	assert.ErrorIs(t, err, template.ErrTemplateNotFound)

	set2, _ := m.GetTemplateSet(setID2)
	source, err := set2.GetTemplateSource("my-template")
	require.NoError(t, err)
	assert.Equal(t, "Hello", source)
}

func TestManager_UpdateTemplateNotFound(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	tplID := registry.NewID("test", "missing")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    registry.NewID("test", "set"),
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	err := m.Update(ctx, tplEntry)
	require.Error(t, err)
	assert.ErrorIs(t, err, template.ErrTemplateNotFound)
}

func TestManager_DeleteTemplate(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	_ = m.Add(ctx, tplEntry)

	err := m.Delete(ctx, tplEntry)
	require.NoError(t, err)

	set, _ := m.GetTemplateSet(setID)
	_, err = set.GetTemplateSource("my-template")
	assert.ErrorIs(t, err, template.ErrTemplateNotFound)
}

func TestManager_DeleteTemplateNotFound(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	tplEntry := registry.Entry{
		ID:   registry.NewID("test", "missing"),
		Kind: template.Jet,
	}
	err := m.Delete(ctx, tplEntry)
	require.Error(t, err)
	assert.ErrorIs(t, err, template.ErrTemplateNotFound)
}

func TestManager_UpdateSet(t *testing.T) {
	m, bus := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	_ = m.Add(ctx, tplEntry)

	updatedSetCfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
		},
	}
	updatedSetEntry := makeSetEntry(setID, updatedSetCfg)
	err := m.Update(ctx, updatedSetEntry)
	require.NoError(t, err)

	events := bus.getEvents()
	var updateEvent *event.Event
	for _, e := range events {
		if e.Kind == resource.Update {
			updateEvent = &e
			break
		}
	}
	require.NotNil(t, updateEvent)

	set, _ := m.GetTemplateSet(setID)
	source, _ := set.GetTemplateSource("my-template")
	assert.Equal(t, "Hello", source)
}

func TestManager_DeleteSet(t *testing.T) {
	m, bus := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	err := m.Delete(ctx, setEntry)
	require.NoError(t, err)

	_, err = m.GetTemplateSet(setID)
	assert.ErrorIs(t, err, template.ErrSetNotFound)

	events := bus.getEvents()
	var deleteEvent *event.Event
	for _, e := range events {
		if e.Kind == resource.Delete {
			deleteEvent = &e
			break
		}
	}
	require.NotNil(t, deleteEvent)
}

func TestManager_DeleteSetNotEmpty(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	tplID := registry.NewID("test", "my-template")
	tplCfg := &template.Config{
		Source: "Hello",
		Set:    setID,
	}
	tplEntry := makeTemplateEntry(tplID, tplCfg)
	_ = m.Add(ctx, tplEntry)

	err := m.Delete(ctx, setEntry)
	require.Error(t, err)
	assert.ErrorIs(t, err, template.ErrSetNotEmpty)
}

func TestManager_UnsupportedKind(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	entry := registry.Entry{
		ID:   registry.NewID("test", "unknown"),
		Kind: "unknown.kind",
	}

	err := m.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown.kind")

	err = m.Update(ctx, entry)
	require.Error(t, err)

	err = m.Delete(ctx, entry)
	require.Error(t, err)
}

func TestManager_Acquire(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	res, err := m.Acquire(ctx, setID, resource.ModeNormal)
	require.NoError(t, err)
	require.NotNil(t, res)

	set, err := res.Get()
	require.NoError(t, err)
	assert.NotNil(t, set)

	res.Release()
}

func TestManager_AcquireNotFound(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	_, err := m.Acquire(ctx, registry.NewID("test", "missing"), resource.ModeNormal)
	require.Error(t, err)
	assert.ErrorIs(t, err, template.ErrTemplateNotFound)
}

func TestManager_ConcurrentOperations(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()

	setID := registry.NewID("test", "my-set")
	setCfg := &template.SetConfig{}
	setEntry := makeSetEntry(setID, setCfg)
	_ = m.Add(ctx, setEntry)

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tplID := registry.NewID("test", "template-"+string(rune('a'+i)))
			tplCfg := &template.Config{
				Source: "Hello",
				Set:    setID,
			}
			tplEntry := makeTemplateEntry(tplID, tplCfg)
			if err := m.Add(ctx, tplEntry); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent add error: %v", err)
	}

	set, _ := m.GetTemplateSet(setID)
	templates := set.GetAllTemplates()
	assert.Len(t, templates, 10)
}

func TestManager_GetTemplateSetNotFound(t *testing.T) {
	m, _ := newTestManager(t)
	_, err := m.GetTemplateSet(registry.NewID("test", "missing"))
	require.Error(t, err)
	assert.ErrorIs(t, err, template.ErrSetNotFound)
}
