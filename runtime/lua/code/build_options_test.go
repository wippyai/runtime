package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

func TestBuildOptions_WithMethods(t *testing.T) {
	opts := NewBuildOptions()

	// Test WithMode
	opts.WithMode(AllowListed)
	assert.Equal(t, AllowListed, opts.Mode)

	// Test WithAllowed
	fooID := registry.ID{Name: "foo"}
	barID := registry.ID{Name: "bar"}
	opts.WithAllowed(fooID, barID)
	assert.True(t, contains(opts.Allowed, fooID))
	assert.True(t, contains(opts.Allowed, barID))
	assert.False(t, contains(opts.Allowed, registry.ID{Name: "baz"}))

	// Test WithDenied
	bazID := registry.ID{Name: "baz"}
	quxID := registry.ID{Name: "qux"}
	opts.WithDenied(bazID, quxID)
	assert.True(t, contains(opts.Denied, bazID))
	assert.True(t, contains(opts.Denied, quxID))
	assert.False(t, contains(opts.Denied, fooID))

	// Test WithRequired
	req1ID := registry.ID{Name: "req1"}
	req2ID := registry.ID{Name: "req2"}
	opts.WithRequired(req1ID, req2ID)
	assert.True(t, contains(opts.Required, req1ID))
	assert.True(t, contains(opts.Required, req2ID))
	assert.False(t, contains(opts.Required, fooID))

	// Test WithPreloaded
	preload1 := Preload{
		Name:     "dep1",
		ModuleID: registry.ID{Name: "node1"},
	}
	preload2 := Preload{
		Name:     "dep2",
		ModuleID: registry.ID{Name: "node2"},
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
}

func TestBuildOptions_StateConsistency(t *testing.T) {
	fooID := registry.ID{Name: "foo"}

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
		assert.Equal(t, "process `:foo` is not allowed in this build", err.Error())
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
		assert.Equal(t, "process `:foo` is not allowed in this build", err.Error())
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
			ModuleID: registry.ID{Name: name},
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
		errorMsg  string
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
				WithRequired(registry.ID{Name: "foo"}),
			wantError: true,
			errorMsg:  "required process `:foo` was not found",
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
				WithAllowed(registry.ID{Name: "foo"}).
				WithRequired(registry.ID{Name: "foo"}),
			wantError: true,
			errorMsg:  "required process `:foo` was not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate(map[registry.ID]*Node{})
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Equal(t, tt.errorMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBuildOptions_ModificationAfterSetup(t *testing.T) {
	fooID := registry.ID{Name: "foo"}
	barID := registry.ID{Name: "bar"}

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
		assert.Equal(t, "process `:foo` is not allowed in this build", err.Error())
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
		errorMsg  string
	}{
		{
			name: "AllowAll mode - allow everything not denied",
			opts: NewBuildOptions().
				WithMode(AllowAll),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "AllowAll mode with denied Process",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithDenied(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "process `:foo` is not allowed in this build",
		},
		{
			name: "AllowListed mode - only allow listed",
			opts: NewBuildOptions().
				WithMode(AllowListed).
				WithAllowed(registry.ID{Name: "foo"}, registry.ID{Name: "bar"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "AllowListed mode - reject unlisted",
			opts: NewBuildOptions().
				WithMode(AllowListed).
				WithAllowed(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "process `:bar` is not in the allowed IDs list",
		},
		{
			name: "DenyAll mode - only allow required",
			opts: NewBuildOptions().
				WithMode(DenyAll).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
			},
			wantError: false,
		},
		{
			name: "DenyAll mode - reject non-required",
			opts: NewBuildOptions().
				WithMode(DenyAll).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "process `:bar` is not allowed (DenyAll mode)",
		},
		{
			name: "StrictListed mode - required must be allowed",
			opts: NewBuildOptions().
				WithMode(StrictListed).
				WithAllowed(registry.ID{Name: "foo"}, registry.ID{Name: "bar"}).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "StrictListed mode - fail if required not allowed",
			opts: NewBuildOptions().
				WithMode(StrictListed).
				WithAllowed(registry.ID{Name: "bar"}).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
				{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "required process `:foo` must also be in allowed list (StrictListed mode)",
		},
		{
			name: "Missing required Process",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "required process `:foo` was not found",
		},
		{
			name: "Denied takes precedence over required",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.ID{Name: "foo"}).
				WithDenied(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				{Name: "foo"}: createNode("foo"),
			},
			wantError: true,
			errorMsg:  "process `:foo` is not allowed in this build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate(tt.nodes)
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Equal(t, tt.errorMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
