package component

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"sort"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/system/eventbus"
	systempayload "github.com/ponyruntime/pony/system/payload"
	jsonpkg "github.com/ponyruntime/pony/system/payload/json"
	"github.com/stretchr/testify/assert"
)

// mockEntityHandler implements EntityHandler for testing
type mockEntityHandler struct {
	onEntryCreate func(context.Context, registry.Entry) error
	onEntryUpdate func(context.Context, registry.Entry) error
	onEntryDelete func(context.Context, registry.Entry) error
	onInvalidate  func(context.Context, []registry.ID)
}

func (m *mockEntityHandler) Add(ctx context.Context, entry registry.Entry) error {
	if m.onEntryCreate != nil {
		return m.onEntryCreate(ctx, entry)
	}
	return nil
}

func (m *mockEntityHandler) Update(ctx context.Context, entry registry.Entry) error {
	if m.onEntryUpdate != nil {
		return m.onEntryUpdate(ctx, entry)
	}
	return nil
}

func (m *mockEntityHandler) Delete(ctx context.Context, entry registry.Entry) error {
	if m.onEntryDelete != nil {
		return m.onEntryDelete(ctx, entry)
	}
	return nil
}

func (m *mockEntityHandler) Invalidate(ctx context.Context, ids []registry.ID) {
	if m.onInvalidate != nil {
		m.onInvalidate(ctx, ids)
	}
}

// ValidatedConfig is a test config struct with validation
type ValidatedConfig struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func (vc *ValidatedConfig) Validate() error {
	if vc.Name == "" {
		return assert.AnError
	}
	if vc.Value <= 0 {
		return assert.AnError
	}
	return nil
}

func TestNewHandler(t *testing.T) {
	entityHandler := &mockEntityHandler{}
	kinds := registry.Kind("test")

	handler := NewHandler(kinds, entityHandler)

	assert.NotNil(t, handler)
	assert.Equal(t, entityHandler, handler.entity)
	assert.NotNil(t, handler.inner)
}

func TestHandler_Pattern(t *testing.T) {
	entityHandler := &mockEntityHandler{}
	kinds := registry.Kind("test")

	handler := NewHandler(kinds, entityHandler)
	pattern := handler.Pattern()

	expectedPattern := eventbus.Pattern{
		System: "(registry|lua)",
		Kind:   "(entry|lua).(create|update|delete|reset_code)",
	}

	assert.Equal(t, expectedPattern.System, pattern.System)
	assert.Equal(t, expectedPattern.Kind, pattern.Kind)
}

func TestHandler_Handle_LuaInvalidateEvent(t *testing.T) {
	entityHandler := &mockEntityHandler{}
	handler := NewHandler(registry.Kind("test"), entityHandler)

	// Track if Invalidate was called
	var invalidatedIDs []registry.ID
	entityHandler.onInvalidate = func(_ context.Context, ids []registry.ID) {
		invalidatedIDs = ids
	}

	event := event.Event{
		System: api.System,
		Kind:   api.InvalidateNodes,
		Data:   []registry.ID{{Name: "test1"}, {Name: "test2"}},
	}

	err := handler.Handle(context.Background(), event)

	assert.NoError(t, err)
}

func TestHandler_Handle_LuaInvalidateEvent_InvalidData(t *testing.T) {
	entityHandler := &mockEntityHandler{}
	handler := NewHandler(registry.Kind("test"), entityHandler)

	// Track if Invalidate was called
	var invalidatedIDs []registry.ID
	entityHandler.onInvalidate = func(_ context.Context, ids []registry.ID) {
		invalidatedIDs = ids
	}

	event := event.Event{
		System: api.System,
		Kind:   api.InvalidateNodes,
		Data:   "invalid data type", // Should be []registry.ID
	}

	err := handler.Handle(context.Background(), event)

	assert.NoError(t, err)
	// Invalidate should not be called with invalid data
	assert.Nil(t, invalidatedIDs)
}

func TestHandler_Handle_UnknownLuaEvent(t *testing.T) {
	entityHandler := &mockEntityHandler{}
	handler := NewHandler(registry.Kind("test"), entityHandler)

	event := event.Event{
		System: api.System,
		Kind:   "unknown.event",
		Data:   "some data",
	}

	err := handler.Handle(context.Background(), event)

	// Should delegate to inner handler
	assert.NoError(t, err)
}

func TestUnpackConfig_ValidConfig(t *testing.T) {
	// Create a test config struct
	type TestConfig struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	// Create test data as JSON string
	testData := `{"name": "test", "value": 42}`

	// Create payload
	payloadData := payload.NewPayload(testData, payload.JSON)

	// Create entry
	entry := registry.Entry{
		Kind: registry.Kind("test"),
		Data: payloadData,
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpkg.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	// Test UnpackConfig
	config, err := UnpackConfig[TestConfig](ctx, entry)

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "test", config.Name)
	assert.Equal(t, 42, config.Value)
}

func TestUnpackConfig_InvalidConfig(t *testing.T) {
	// Create test data with invalid JSON that will cause unmarshaling error
	testData := `{"name": "test", "value": "not_a_number"}`

	// Create payload
	payloadData := payload.NewPayload(testData, payload.JSON)

	// Create entry
	entry := registry.Entry{
		Kind: registry.Kind("test"),
		Data: payloadData,
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpkg.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	// Test UnpackConfig with invalid data (string where int expected)
	type TestConfig struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	config, err := UnpackConfig[TestConfig](ctx, entry)

	assert.Error(t, err)
	assert.Nil(t, config)
}

func TestUnpackConfig_NoTranscoder(t *testing.T) {
	entry := registry.Entry{
		Kind: registry.Kind("test"),
	}

	ctx := ctxapi.NewRootContext() // No transcoder in context

	type TestConfig struct {
		Name string `json:"name"`
	}

	config, err := UnpackConfig[TestConfig](ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transcoder not found in context")
	assert.Nil(t, config)
}

func TestUnpackConfig_WithValidation(t *testing.T) {
	// Test with valid config
	testData := `{"name": "test", "value": 42}`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: registry.Kind("test"),
		Data: payloadData,
	}

	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpkg.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	config, err := UnpackConfig[ValidatedConfig](ctx, entry)

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "test", config.Name)
	assert.Equal(t, 42, config.Value)

	// Test with invalid config (empty name)
	testDataInvalid := `{"name": "", "value": 42}`

	payloadDataInvalid := payload.NewPayload(testDataInvalid, payload.JSON)
	entryInvalid := registry.Entry{
		Kind: registry.Kind("test"),
		Data: payloadDataInvalid,
	}

	configInvalid, err := UnpackConfig[ValidatedConfig](ctx, entryInvalid)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
	assert.Nil(t, configInvalid)
}

func TestBuildImports(t *testing.T) {
	tests := []struct {
		name     string
		imports  map[string]registry.ID
		modules  []string
		expected []code.Import
	}{
		{
			name:     "empty imports and modules",
			imports:  map[string]registry.ID{},
			modules:  []string{},
			expected: []code.Import{},
		},
		{
			name: "only imports",
			imports: map[string]registry.ID{
				"alias1": {Name: "module1"},
				"alias2": {Name: "module2"},
			},
			modules: []string{},
			expected: []code.Import{
				{ID: registry.ID{Name: "module1"}, Alias: "alias1"},
				{ID: registry.ID{Name: "module2"}, Alias: "alias2"},
			},
		},
		{
			name:    "only modules",
			imports: map[string]registry.ID{},
			modules: []string{"module1", "module2"},
			expected: []code.Import{
				{ID: registry.ID{Name: "module1"}, Alias: "module1"},
				{ID: registry.ID{Name: "module2"}, Alias: "module2"},
			},
		},
		{
			name: "both imports and modules",
			imports: map[string]registry.ID{
				"alias1": {Name: "module1"},
			},
			modules: []string{"module2"},
			expected: []code.Import{
				{ID: registry.ID{Name: "module1"}, Alias: "alias1"},
				{ID: registry.ID{Name: "module2"}, Alias: "module2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildImports(tt.imports, tt.modules)

			assert.Len(t, result, len(tt.expected))

			// Sort both result and expected by alias for order-independent comparison
			sort.Slice(result, func(i, j int) bool {
				return result[i].Alias < result[j].Alias
			})
			sort.Slice(tt.expected, func(i, j int) bool {
				return tt.expected[i].Alias < tt.expected[j].Alias
			})

			for i, expected := range tt.expected {
				assert.Equal(t, expected.Alias, result[i].Alias)
			}
		})
	}
}

func TestBuildImports_OrderIndependence(t *testing.T) {
	// This test demonstrates that BuildImports can produce different orders
	// but our test handles it correctly by sorting

	imports := map[string]registry.ID{
		"alias1": {Name: "module1"},
		"alias2": {Name: "module2"},
	}

	// Run multiple times to show different orders can be produced
	results := make([][]code.Import, 10)
	for i := 0; i < 10; i++ {
		results[i] = BuildImports(imports, []string{})
	}

	// All results should have the same length
	for i := 1; i < len(results); i++ {
		assert.Len(t, results[i], len(results[0]))
	}

	// Sort all results and verify they're identical
	sortedResults := make([][]code.Import, len(results))
	for i, result := range results {
		sorted := make([]code.Import, len(result))
		copy(sorted, result)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Alias < sorted[j].Alias
		})
		sortedResults[i] = sorted
	}

	// All sorted results should be identical
	for i := 1; i < len(sortedResults); i++ {
		assert.Equal(t, sortedResults[0], sortedResults[i])
	}

	// Verify the sorted result contains the expected imports
	expected := []code.Import{
		{ID: registry.ID{Name: "module1"}, Alias: "alias1"},
		{ID: registry.ID{Name: "module2"}, Alias: "alias2"},
	}
	sort.Slice(expected, func(i, j int) bool {
		return expected[i].Alias < expected[j].Alias
	})

	assert.Equal(t, expected, sortedResults[0])
}
