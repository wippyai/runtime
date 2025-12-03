package code

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Test helper functions for slice containment checks
func contains(slice []registry.ID, item registry.ID) bool {
	for _, id := range slice {
		if id == item {
			return true
		}
	}
	return false
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestBuildOptions_WithMethods(t *testing.T) {
	opts := NewBuildOptions()

	// Test WithMode
	opts.WithMode(AllowListed)
	assert.Equal(t, AllowListed, opts.Mode)

	// Test WithAllowed
	fooID := registry.NewID("", "foo")
	barID := registry.NewID("", "bar")
	opts.WithAllowed(fooID, barID)
	assert.True(t, contains(opts.Allowed, fooID))
	assert.True(t, contains(opts.Allowed, barID))
	assert.False(t, contains(opts.Allowed, registry.NewID("", "baz")))

	// Test WithDenied
	bazID := registry.NewID("", "baz")
	quxID := registry.NewID("", "qux")
	opts.WithDenied(bazID, quxID)
	assert.True(t, contains(opts.Denied, bazID))
	assert.True(t, contains(opts.Denied, quxID))
	assert.False(t, contains(opts.Denied, fooID))

	// Test WithRequired
	req1ID := registry.NewID("", "req1")
	req2ID := registry.NewID("", "req2")
	opts.WithRequired(req1ID, req2ID)
	assert.True(t, contains(opts.Required, req1ID))
	assert.True(t, contains(opts.Required, req2ID))
	assert.False(t, contains(opts.Required, fooID))

	// Test WithPreloaded
	preload1 := Preload{
		Name:     "dep1",
		ModuleID: registry.NewID("", "node1"),
	}
	preload2 := Preload{
		Name:     "dep2",
		ModuleID: registry.NewID("", "node2"),
	}
	opts.WithPreloaded(preload1, preload2)
	assert.Len(t, opts.Preloaded, 2)
	assert.Equal(t, preload1, opts.Preloaded[0])
	assert.Equal(t, preload2, opts.Preloaded[1])

	// Test chaining
	opts2 := NewBuildOptions().
		WithMode(DenyAll).
		WithRequired(fooID).
		WithAllowed(barID)

	assert.Equal(t, DenyAll, opts2.Mode)
	assert.True(t, contains(opts2.Required, fooID))
	assert.True(t, contains(opts2.Allowed, barID))

	// Test WithDeniedClasses
	opts3 := NewBuildOptions().
		WithDeniedClasses(luaapi.ClassNetwork, luaapi.ClassIO)
	assert.Len(t, opts3.DeniedClasses, 2)
	assert.True(t, containsString(opts3.DeniedClasses, luaapi.ClassNetwork))
	assert.True(t, containsString(opts3.DeniedClasses, luaapi.ClassIO))

	// Test WithAllowedClasses
	opts4 := NewBuildOptions().
		WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassEncoding)
	assert.Len(t, opts4.AllowedClasses, 2)
	assert.True(t, containsString(opts4.AllowedClasses, luaapi.ClassDeterministic))
	assert.True(t, containsString(opts4.AllowedClasses, luaapi.ClassEncoding))
}

// mockModule implements luaapi.Module for testing
type mockModule struct {
	name    string
	classes []string
}

func (m *mockModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        m.name,
		Description: "test module",
		Class:       m.classes,
	}
}

func (m *mockModule) Loader(_ *lua.LState) int {
	return 0
}

func (m *mockModule) Register(_ *lua.LState) *luaapi.Registration {
	return nil
}

func TestBuildOptions_ClassFiltering(t *testing.T) {
	netModID := registry.NewID("", "network_mod")
	ioModID := registry.NewID("", "io_mod")
	deterministicModID := registry.NewID("", "deterministic_mod")

	netModule := &mockModule{name: "network_mod", classes: []string{luaapi.ClassNetwork}}
	ioModule := &mockModule{name: "io_mod", classes: []string{luaapi.ClassIO}}
	deterministicModule := &mockModule{name: "deterministic_mod", classes: []string{luaapi.ClassDeterministic, luaapi.ClassEncoding}}

	t.Run("deny network class", func(t *testing.T) {
		opts := NewBuildOptions().
			WithDeniedClasses(luaapi.ClassNetwork)

		nodes := map[registry.ID]*Node{
			netModID: {ID: netModID, Kind: luaapi.KindModule, Module: netModule},
		}

		err := opts.Validate(nodes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "denied class")
	})

	t.Run("deny multiple classes", func(t *testing.T) {
		opts := NewBuildOptions().
			WithDeniedClasses(luaapi.ClassNetwork, luaapi.ClassIO)

		nodes := map[registry.ID]*Node{
			ioModID: {ID: ioModID, Kind: luaapi.KindModule, Module: ioModule},
		}

		err := opts.Validate(nodes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "denied class")
	})

	t.Run("allow only deterministic", func(t *testing.T) {
		opts := NewBuildOptions().
			WithAllowedClasses(luaapi.ClassDeterministic)

		nodes := map[registry.ID]*Node{
			deterministicModID: {ID: deterministicModID, Kind: luaapi.KindModule, Module: deterministicModule},
		}

		err := opts.Validate(nodes)
		assert.NoError(t, err)
	})

	t.Run("reject non-deterministic when only deterministic allowed", func(t *testing.T) {
		opts := NewBuildOptions().
			WithAllowedClasses(luaapi.ClassDeterministic)

		nodes := map[registry.ID]*Node{
			netModID: {ID: netModID, Kind: luaapi.KindModule, Module: netModule},
		}

		err := opts.Validate(nodes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not have any allowed class")
	})

	t.Run("mixed modules with class filtering", func(t *testing.T) {
		opts := NewBuildOptions().
			WithDeniedClasses(luaapi.ClassNetwork)

		nodes := map[registry.ID]*Node{
			deterministicModID: {ID: deterministicModID, Kind: luaapi.KindModule, Module: deterministicModule},
			ioModID:            {ID: ioModID, Kind: luaapi.KindModule, Module: ioModule},
		}

		err := opts.Validate(nodes)
		assert.NoError(t, err)
	})

	t.Run("non-module nodes are not affected by class filtering", func(t *testing.T) {
		opts := NewBuildOptions().
			WithDeniedClasses(luaapi.ClassNetwork)

		funcID := registry.NewID("", "some_func")
		nodes := map[registry.ID]*Node{
			funcID: {ID: funcID, Kind: luaapi.KindFunction, Source: "return 1"},
		}

		err := opts.Validate(nodes)
		assert.NoError(t, err)
	})
}

func TestBuildOptions_StateConsistency(t *testing.T) {
	fooID := registry.NewID("", "foo")

	t.Run("same Process in allowed and denied", func(t *testing.T) {
		opts := NewBuildOptions().
			WithMode(AllowListed).
			WithAllowed(fooID).
			WithDenied(fooID)

		nodes := map[registry.ID]*Node{
			fooID: {ID: fooID},
		}
		err := opts.Validate(nodes)
		assert.Error(t, err)
		var apiErr apierror.Error
		assert.True(t, errors.As(err, &apiErr), "error should implement apierror.Error")
		assert.Equal(t, apierror.KindPermissionDenied, apiErr.Kind())
	})

	t.Run("same Process in required and denied", func(t *testing.T) {
		opts := NewBuildOptions().
			WithMode(AllowAll).
			WithRequired(fooID).
			WithDenied(fooID)

		nodes := map[registry.ID]*Node{
			fooID: {ID: fooID},
		}
		err := opts.Validate(nodes)
		assert.Error(t, err)
		var apiErr apierror.Error
		assert.True(t, errors.As(err, &apiErr), "error should implement apierror.Error")
		assert.Equal(t, apierror.KindPermissionDenied, apiErr.Kind())
	})

	t.Run("same Process added multiple times to lists", func(t *testing.T) {
		opts := NewBuildOptions()

		// AddCleanup same Process multiple times to lists
		opts.WithAllowed(fooID, fooID)
		assert.True(t, contains(opts.Allowed, fooID))

		opts.WithDenied(fooID, fooID)
		assert.True(t, contains(opts.Denied, fooID))

		opts.WithRequired(fooID, fooID)
		assert.True(t, contains(opts.Required, fooID))
	})
}

func TestBuildOptions_PreloadedDependencies(t *testing.T) {
	// Helper function to create test preloads
	createPreload := func(name string) Preload {
		return Preload{
			Name:     name,
			ModuleID: registry.NewID("", name),
		}
	}

	preload1 := createPreload("preload1")
	preload2 := createPreload("preload2")

	tests := []struct {
		name          string
		preloaded     []Preload
		expectedCount int
	}{
		{
			name:          "no preloaded dependencies",
			preloaded:     []Preload{},
			expectedCount: 0,
		},
		{
			name:          "single preloaded dependency",
			preloaded:     []Preload{preload1},
			expectedCount: 1,
		},
		{
			name:          "multiple preloaded dependencies",
			preloaded:     []Preload{preload1, preload2},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := NewBuildOptions().WithPreloaded(tt.preloaded...)
			assert.Equal(t, tt.expectedCount, len(opts.Preloaded))

			// Verify preloaded dependencies
			for i, dep := range tt.preloaded {
				assert.Equal(t, dep, opts.Preloaded[i])
			}
		})
	}
}

func TestBuildOptions_EmptyNodes(t *testing.T) {
	tests := []struct {
		name      string
		opts      *BuildOptions
		wantError bool
		errorKind apierror.Kind
	}{
		{
			name: "empty nodes with no requirements",
			opts: NewBuildOptions().
				WithMode(AllowAll),
			wantError: false,
		},
		{
			name: "empty nodes with required IDs",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.NewID("", "foo")),
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
		{
			name: "empty nodes in DenyAll mode",
			opts: NewBuildOptions().
				WithMode(DenyAll),
			wantError: false,
		},
		{
			name: "empty nodes in StrictListed mode",
			opts: NewBuildOptions().
				WithMode(StrictListed).
				WithAllowed(registry.NewID("", "foo")).
				WithRequired(registry.NewID("", "foo")),
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate(map[registry.ID]*Node{})
			if tt.wantError {
				assert.Error(t, err)
				var apiErr apierror.Error
				assert.True(t, errors.As(err, &apiErr), "error should implement apierror.Error")
				assert.Equal(t, tt.errorKind, apiErr.Kind())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBuildOptions_ModificationAfterSetup(t *testing.T) {
	fooID := registry.NewID("", "foo")
	barID := registry.NewID("", "bar")

	opts := NewBuildOptions().
		WithMode(AllowListed).
		WithAllowed(fooID)

	// Initial validation
	nodes := map[registry.ID]*Node{
		fooID: {ID: fooID},
	}
	assert.NoError(t, opts.Validate(nodes))

	// AddCleanup new allowed Process and test
	opts.WithAllowed(barID)
	nodes[barID] = &Node{ID: barID}
	assert.NoError(t, opts.Validate(nodes))

	// AddCleanup denied Process and test
	opts.WithDenied(fooID)
	err := opts.Validate(nodes)
	if assert.Error(t, err) {
		var apiErr apierror.Error
		assert.True(t, errors.As(err, &apiErr), "error should implement apierror.Error")
		assert.Equal(t, apierror.KindPermissionDenied, apiErr.Kind())
	}
}

func TestBuildOptions_Validate(t *testing.T) {
	// Helper function to create test nodes
	createNode := func(name string) *Node {
		return &Node{
			Kind:   "function.lua",
			Source: "function " + name + "() return 'test' end",
			Method: "test",
		}
	}

	tests := []struct {
		name      string
		opts      *BuildOptions
		nodes     map[registry.ID]*Node
		wantError bool
		errorKind apierror.Kind
	}{
		{
			name: "AllowAll mode - allow everything not denied",
			opts: NewBuildOptions().
				WithMode(AllowAll),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "AllowAll mode with denied Process",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithDenied(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
		{
			name: "AllowListed mode - only allow listed",
			opts: NewBuildOptions().
				WithMode(AllowListed).
				WithAllowed(registry.NewID("", "foo"), registry.NewID("", "bar")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "AllowListed mode - reject unlisted",
			opts: NewBuildOptions().
				WithMode(AllowListed).
				WithAllowed(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
		{
			name: "DenyAll mode - only allow required",
			opts: NewBuildOptions().
				WithMode(DenyAll).
				WithRequired(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
			},
			wantError: false,
		},
		{
			name: "DenyAll mode - reject non-required",
			opts: NewBuildOptions().
				WithMode(DenyAll).
				WithRequired(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
		{
			name: "StrictListed mode - required must be allowed",
			opts: NewBuildOptions().
				WithMode(StrictListed).
				WithAllowed(registry.NewID("", "foo"), registry.NewID("", "bar")).
				WithRequired(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "StrictListed mode - fail if required not allowed",
			opts: NewBuildOptions().
				WithMode(StrictListed).
				WithAllowed(registry.NewID("", "bar")).
				WithRequired(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
		{
			name: "Missing required Process",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "bar"): createNode("bar"),
			},
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
		{
			name: "Denied takes precedence over required",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.NewID("", "foo")).
				WithDenied(registry.NewID("", "foo")),
			nodes: map[registry.ID]*Node{
				registry.NewID("", "foo"): createNode("foo"),
			},
			wantError: true,
			errorKind: apierror.KindPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate(tt.nodes)
			if tt.wantError {
				assert.Error(t, err)
				var apiErr apierror.Error
				assert.True(t, errors.As(err, &apiErr), "error should implement apierror.Error")
				assert.Equal(t, tt.errorKind, apiErr.Kind())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
