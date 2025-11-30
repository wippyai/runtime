package template

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	templatecfg "github.com/wippyai/runtime/api/service/template"
	lua2payload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	templatesvc "github.com/wippyai/runtime/service/template"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	lua "github.com/yuin/gopher-lua"
)

// mockResource implements the resource.Resource interface for testing
type mockResource struct {
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() {
	m.released = true
}

// mockResourceRegistry is a simple mock for the resource registry
type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Acquire(
	_ context.Context,
	id registry.ID,
	_ resource.AccessMode,
) (resource.Resource[any], error) {
	res, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return res, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

// createTestTemplateSet creates a template set for testing
func createTestTemplateSet(t *testing.T) *templatesvc.TemplateSet {
	cfg := &templatecfg.SetConfig{
		Engine: templatecfg.EngineConfig{
			DevelopmentMode: true,
			Delimiters: templatecfg.DelimiterConfig{
				Left:  "{{",
				Right: "}}",
			},
			Globals: map[string]any{
				"siteName": "Test Site",
				"version":  "1.0",
			},
		},
	}

	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	id := registry.ID{Name: "test-templates"}
	set, err := templatesvc.NewTemplateSet(id, cfg, transcoder)
	require.NoError(t, err)

	err = set.AddTemplate("welcome", "Hello, {{ name }}!")
	require.NoError(t, err)

	err = set.AddTemplate("profile", "User: {{ user.name }}, Age: {{ user.age }}, Email: {{ user.email }}")
	require.NoError(t, err)

	err = set.AddTemplate("list", "Items: {{ range i, item := items }}{{ if i > 0 }}, {{ end }}{{ item }}{{ end }}")
	require.NoError(t, err)

	err = set.AddTemplate("global", "Site: {{ siteName }}, Version: {{ version }}")
	require.NoError(t, err)

	return set
}

func setupTestState(t *testing.T, mockRes *mockResource) *lua.LState {
	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)
	lua2payload.Register(transcoder)

	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_templates"): mockRes,
		},
	}

	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, transcoder)
	ctx = resource.WithRegistry(ctx, mockRegistry)

	l := lua.NewState()
	l.SetContext(ctx)
	lua2api.LoadModule(l, Module)

	return l
}

func TestTemplateGet(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end
		local ok = tmpl:release()
		if not ok then error("release failed") end
	`)
	require.NoError(t, err)
	assert.True(t, mockRes.released, "Template resource was not released")
}

func TestTemplateRender(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		local result, err = tmpl:render("welcome", {name = "John"})
		if err then error(err) end

		tmpl:release()
		if result ~= "Hello, John!" then
			error("expected 'Hello, John!' got '" .. result .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestTemplateRenderWithNestedData(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		local result, err = tmpl:render("profile", {
			user = {
				name = "Alice",
				age = 30,
				email = "alice@example.com"
			}
		})
		if err then error(err) end

		tmpl:release()
		local expected = "User: Alice, Age: 30, Email: alice@example.com"
		if result ~= expected then
			error("expected '" .. expected .. "' got '" .. result .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestTemplateRenderWithArray(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		local result, err = tmpl:render("list", {
			items = {"apple", "banana", "orange"}
		})
		if err then error(err) end

		tmpl:release()
		if result ~= "Items: apple, banana, orange" then
			error("expected 'Items: apple, banana, orange' got '" .. result .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestTemplateRenderWithGlobals(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		local result, err = tmpl:render("global", {})
		if err then error(err) end

		tmpl:release()
		if result ~= "Site: Test Site, Version: 1.0" then
			error("expected 'Site: Test Site, Version: 1.0' got '" .. result .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestTemplateRenderNotFound(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		local result, err = tmpl:render("nonexistent", {})

		tmpl:release()
		if err ~= "template not found" then
			error("expected 'template not found' got '" .. tostring(err) .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestTemplateRenderAfterRelease(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		tmpl:release()

		local result, err = tmpl:render("welcome", {name = "Test"})
		if err ~= "template set is released" then
			error("expected 'template set is released' got '" .. tostring(err) .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestTemplateSetToString(t *testing.T) {
	templateSet := createTestTemplateSet(t)
	mockRes := &mockResource{resValue: templateSet}
	l := setupTestState(t, mockRes)
	defer l.Close()

	err := l.DoString(`
		local tmpl, err = templates.get("app:test_templates")
		if err then error(err) end

		local str1 = tostring(tmpl)
		tmpl:release()
		local str2 = tostring(tmpl)

		if str1 ~= "template.Set{}" then
			error("expected 'template.Set{}' got '" .. str1 .. "'")
		end
		if str2 ~= "template.Set{released}" then
			error("expected 'template.Set{released}' got '" .. str2 .. "'")
		end
	`)
	require.NoError(t, err)
}

func TestModuleInfo(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "templates", info.Name)
	assert.Equal(t, "Template rendering engine", info.Description)
}
