package template

import (
	"context"
	templatesvc "github.com/ponyruntime/pony/service/template"
	lua2 "github.com/ponyruntime/pony/system/payload/lua"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/service/template"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	payloadSystem "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
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
	ctx context.Context,
	id registry.ID,
	mode resource.AccessMode,
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

// setupLuaWithTemplates sets up a Lua state with our template module and a test template set
func setupLuaWithTemplates(t *testing.T, mockRes *mockResource) (*engine.CoroutineVM, *lua.LState, engine.UnitOfWork, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create the template module
	module := NewTemplateModule(logger)

	// Create a mock resource registry with our test template set
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_templates"): mockRes,
		},
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the template module
	L.PreloadModule(module.Name(), module.Loader)

	// Set up a transcoder to convert between Lua and Go values
	dtt := payloadSystem.GlobalTranscoder()
	json.Register(dtt)
	lua2.Register(dtt)
	ctx := payload.WithTranscoder(context.Background(), dtt)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(ctx)

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	return vm, L, uw, runner
}

// createTestTemplateSet creates a template set for testing
func createTestTemplateSet(t *testing.T) *templatesvc.TemplateSet {
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
			Delimiters: template.DelimiterConfig{
				Left:  "{{",
				Right: "}}",
			},
			Globals: map[string]any{
				"siteName": "Test Site",
				"version":  "1.0",
			},
		},
	}

	// Use real transcoder with json support
	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	id := registry.ID{Name: "test-templates"}
	set, err := templatesvc.NewTemplateSet(id, cfg, transcoder)
	require.NoError(t, err)

	// Add some test templates
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

// TestTemplateBasicGet tests the templates.get function retrieves a template set correctly
func TestTemplateBasicGet(t *testing.T) {
	// Create a test template set
	templateSet := createTestTemplateSet(t)

	// Create our resource that will be tracked for release
	mockRes := &mockResource{
		resValue: templateSet,
	}

	// Setup Lua with the test templates
	vm, L, uw, runner := setupLuaWithTemplates(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_template_get()
			local templates = require("templates")
			local tmpl = templates.get("app:test_templates")
			
			-- Test the connection is valid
			local result = {}
			
			-- Release the templates
			local ok = tmpl:release()
			
			return ok
		end
	`, "test", "test_template_get")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_template_get")
	require.NoError(t, err, "Lua execution failed")

	assert.Equal(t, lua.LTrue, result, "Templates.get should return true on successful release")
	assert.True(t, mockRes.released, "Template resource was not released")
}

// TestTemplateRender tests the template:render method renders a template correctly
func TestTemplateRender(t *testing.T) {
	// Create a test template set
	templateSet := createTestTemplateSet(t)

	// Create our resource
	mockRes := &mockResource{
		resValue: templateSet,
	}

	// Setup Lua with the test templates
	vm, L, uw, runner := setupLuaWithTemplates(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_template_render()
			local templates = require("templates")
			local tmpl = templates.get("app:test_templates")
			
			-- Render a simple template
			local result1, err1 = tmpl:render("welcome", {
				name = "John"
			})
			if err1 then error(err1) end
			
			-- Render a template with nested variables
			local result2, err2 = tmpl:render("profile", {
				user = {
					name = "Alice",
					age = 30,
					email = "alice@example.com"
				}
			})
			if err2 then error(err2) end
			
			-- Render a template with an array
			local result3, err3 = tmpl:render("list", {
				items = {"apple", "banana", "orange"}
			})
			if err3 then error(err3) end
			
			-- Render a template with global variables
			local result4, err4 = tmpl:render("global", {})
			if err4 then error(err4) end
			
			-- Render a non-existent template
			local result5, err5 = tmpl:render("nonexistent", {})
			
			-- Release the templates
			tmpl:release()
			
			return {
				welcome = result1,
				profile = result2,
				list = result3,
				global = result4,
				nonexistent_result = result5,
				nonexistent_error = err5
			}
		end
	`, "test", "test_template_render")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_template_render")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)

	// Verify the rendered templates
	welcome := resultTable.RawGetString("welcome").String()
	assert.Equal(t, "Hello, John!", welcome, "Welcome template should be rendered correctly")

	profile := resultTable.RawGetString("profile").String()
	assert.Equal(t, "User: Alice, Age: 30, Email: alice@example.com", profile, "Profile template should be rendered correctly")

	list := resultTable.RawGetString("list").String()
	assert.Equal(t, "Items: apple, banana, orange", list, "List template should be rendered correctly")

	global := resultTable.RawGetString("global").String()
	assert.Equal(t, "Site: Test Site, Version: 1.0", global, "Global template should be rendered correctly")

	nonexistentResult := resultTable.RawGetString("nonexistent_result")
	assert.Equal(t, lua.LNil, nonexistentResult, "Nonexistent template should return nil")

	nonexistentError := resultTable.RawGetString("nonexistent_error").String()
	assert.Equal(t, "template not found", nonexistentError, "Nonexistent template should return error")
}

// TestTemplateAutomaticRelease tests that template resources are automatically released with UoW
func TestTemplateAutomaticRelease(t *testing.T) {
	// Create a test template set
	templateSet := createTestTemplateSet(t)

	// Create our resource
	mockRes := &mockResource{
		resValue: templateSet,
	}

	// Setup Lua with the test templates
	vm, L, uw, runner := setupLuaWithTemplates(t, mockRes)
	defer vm.Close()

	// Import our test function
	err := vm.Import(`
		function test_template_auto_release()
			local templates = require("templates")
			local tmpl = templates.get("app:test_templates")
			
			-- Render a template
			local result, err = tmpl:render("welcome", {
				name = "Automatic Release Test"
			})
			if err then error(err) end
			
			-- We don't explicitly release the template
			return result
		end
	`, "test", "test_template_auto_release")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_template_auto_release")
	require.NoError(t, err, "Lua execution failed")

	// Verify the rendered template
	assert.Equal(t, "Hello, Automatic Release Test!", result.String(), "Template should be rendered correctly")

	// Close the unit of work which should release all resources
	err = uw.Close()
	assert.NoError(t, err, "Unit of work cleanup failed")

	// Verify the resource was released
	assert.True(t, mockRes.released, "Template resource should be automatically released")
}

// TestTemplateError tests error handling in the template module
func TestTemplateError(t *testing.T) {
	// Create a mock resource registry with no templates
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{},
	}

	logger := zaptest.NewLogger(t)
	module := NewTemplateModule(logger)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the template module
	L.PreloadModule(module.Name(), module.Loader)

	// Set up the context
	ctx := context.Background()
	dtt := payloadSystem.GlobalTranscoder()
	json.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(ctx)
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Add the empty resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Import our test function that should trigger an error
	err = vm.Import(`
		function test_template_get_error()
			local templates = require("templates")
			local ok, err = pcall(function()
				local tmpl = templates.get("nonexistent:templates")
				return tmpl
			end)
			return {success = ok, error = not ok}
		end
	`, "test", "test_template_get_error")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_template_get_error")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)
	success := resultTable.RawGetString("success")
	hasError := resultTable.RawGetString("error")

	assert.Equal(t, lua.LBool(false), success, "Function should fail")
	assert.Equal(t, lua.LBool(true), hasError, "Error should be returned")
}

// TestTemplateModuleIntegration tests the full integration of all module functions
func TestTemplateModuleIntegration(t *testing.T) {
	// Create a test template set
	templateSet := createTestTemplateSet(t)

	// Create our resource
	mockRes := &mockResource{
		resValue: templateSet,
	}

	// Setup Lua with the test templates
	vm, L, uw, runner := setupLuaWithTemplates(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_template_integration()
			local templates = require("templates")
			local results = {}
			
			-- Get the template set
			local tmpl = templates.get("app:test_templates")
			
			-- Render multiple templates with different data types
			local welcome, err = tmpl:render("welcome", {name = "Integration Test"})
			if err then error(err) end
			results.welcome = welcome
			
			-- Test with complex nested data
			local complex, err = tmpl:render("profile", {
				user = {
					name = "Complex User",
					age = 25,
					email = "complex@example.com"
				}
			})
			if err then error(err) end
			results.complex = complex
			
			-- Release the template
			local ok = tmpl:release()
			results.released = ok
			
			-- Trying to render after release should fail
			local success, render_err = pcall(function()
				tmpl:render("welcome", {name = "After Release"})
			end)
			results.render_after_release_success = success
			
			return results
		end
	`, "test", "test_template_integration")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_template_integration")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)

	welcome := resultTable.RawGetString("welcome").String()
	assert.Equal(t, "Hello, Integration Test!", welcome, "Welcome template should be rendered correctly")

	complex := resultTable.RawGetString("complex").String()
	assert.Equal(t, "User: Complex User, Age: 25, Email: complex@example.com", complex, "Complex template should be rendered correctly")

	released := resultTable.RawGetString("released")
	assert.Equal(t, lua.LTrue, released, "Template should be released successfully")

	renderAfterReleaseSuccess := resultTable.RawGetString("render_after_release_success")
	assert.Equal(t, lua.LBool(false), renderAfterReleaseSuccess, "Rendering after release should fail")

	assert.True(t, mockRes.released, "Template resource should be released")
}
