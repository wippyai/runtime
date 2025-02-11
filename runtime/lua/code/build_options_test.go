package lua

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestBuildOptions_Validate(t *testing.T) {
	// Helper function to create test nodes
	createNode := func(name string) *Node {
		return &Node{
			ID:     registry.ID{Name: name},
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
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "AllowAll mode with denied ID",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithDenied(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "ID :foo is not allowed in this build",
		},
		{
			name: "AllowListed mode - only allow listed",
			opts: NewBuildOptions().
				WithMode(AllowListed).
				WithAllowed(registry.ID{Name: "foo"}, registry.ID{Name: "bar"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: false,
		},
		{
			name: "AllowListed mode - reject unlisted",
			opts: NewBuildOptions().
				WithMode(AllowListed).
				WithAllowed(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "ID :bar is not in the allowed IDs list",
		},
		{
			name: "DenyAll mode - only allow required",
			opts: NewBuildOptions().
				WithMode(DenyAll).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
			},
			wantError: false,
		},
		{
			name: "DenyAll mode - reject non-required",
			opts: NewBuildOptions().
				WithMode(DenyAll).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "ID :bar is not allowed (DenyAll mode)",
		},
		{
			name: "StrictListed mode - required must be allowed",
			opts: NewBuildOptions().
				WithMode(StrictListed).
				WithAllowed(registry.ID{Name: "foo"}, registry.ID{Name: "bar"}).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
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
				registry.ID{Name: "foo"}: createNode("foo"),
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "required ID :foo must also be in allowed list (StrictListed mode)",
		},
		{
			name: "Missing required ID",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "bar"}: createNode("bar"),
			},
			wantError: true,
			errorMsg:  "required ID :foo was not found",
		},
		{
			name: "Denied takes precedence over required",
			opts: NewBuildOptions().
				WithMode(AllowAll).
				WithRequired(registry.ID{Name: "foo"}).
				WithDenied(registry.ID{Name: "foo"}),
			nodes: map[registry.ID]*Node{
				registry.ID{Name: "foo"}: createNode("foo"),
			},
			wantError: true,
			errorMsg:  "ID :foo is not allowed in this build",
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

func TestBuildOptions_WithMethods(t *testing.T) {
	opts := NewBuildOptions()

	// Test WithMode
	opts.WithMode(AllowListed)
	assert.Equal(t, AllowListed, opts.Mode)

	// Test WithAllowed
	fooID := registry.ID{Name: "foo"}
	barID := registry.ID{Name: "bar"}
	opts.WithAllowed(fooID, barID)
	assert.True(t, opts.Allowed[fooID])
	assert.True(t, opts.Allowed[barID])
	assert.False(t, opts.Allowed[registry.ID{Name: "baz"}])

	// Test WithDenied
	bazID := registry.ID{Name: "baz"}
	quxID := registry.ID{Name: "qux"}
	opts.WithDenied(bazID, quxID)
	assert.True(t, opts.Denied[bazID])
	assert.True(t, opts.Denied[quxID])
	assert.False(t, opts.Denied[fooID])

	// Test WithRequired
	req1ID := registry.ID{Name: "req1"}
	req2ID := registry.ID{Name: "req2"}
	opts.WithRequired(req1ID, req2ID)
	assert.True(t, opts.Required[req1ID])
	assert.True(t, opts.Required[req2ID])
	assert.False(t, opts.Required[fooID])

	// Test WithPreloaded
	node1 := &Node{ID: registry.ID{Name: "node1"}}
	node2 := &Node{ID: registry.ID{Name: "node2"}}
	dep1 := Dependency{Name: "dep1", Node: node1}
	dep2 := Dependency{Name: "dep2", Node: node2}
	opts.WithPreloaded(dep1, dep2)
	assert.Len(t, opts.Preloaded, 2)
	assert.Equal(t, dep1, opts.Preloaded[0])
	assert.Equal(t, dep2, opts.Preloaded[1])

	// Test chaining
	opts2 := NewBuildOptions().
		WithMode(DenyAll).
		WithRequired(fooID).
		WithAllowed(barID)

	assert.Equal(t, DenyAll, opts2.Mode)
	assert.True(t, opts2.Required[fooID])
	assert.True(t, opts2.Allowed[barID])
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
			errorMsg:  "required ID :foo was not found",
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
			errorMsg:  "required ID :foo was not found",
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

func TestBuildOptions_PreloadedDependencies(t *testing.T) {
	// Helper function to create test nodes and dependencies
	createNodeAndDep := func(name string) (*Node, Dependency) {
		node := &Node{
			ID:     registry.ID{Name: name},
			Kind:   "function.lua",
			Source: "function " + name + "() return 'test' end",
			Method: "test",
		}
		return node, Dependency{Name: name, Node: node}
	}

	_, dep1 := createNodeAndDep("preload1")
	_, dep2 := createNodeAndDep("preload2")

	tests := []struct {
		name          string
		preloaded     []Dependency
		expectedCount int
	}{
		{
			name:          "no preloaded dependencies",
			preloaded:     []Dependency{},
			expectedCount: 0,
		},
		{
			name:          "single preloaded dependency",
			preloaded:     []Dependency{dep1},
			expectedCount: 1,
		},
		{
			name:          "multiple preloaded dependencies",
			preloaded:     []Dependency{dep1, dep2},
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

func TestBuildOptions_StateConsistency(t *testing.T) {
	fooID := registry.ID{Name: "foo"}

	t.Run("same ID in allowed and denied", func(t *testing.T) {
		opts := NewBuildOptions().
			WithMode(AllowListed).
			WithAllowed(fooID).
			WithDenied(fooID)

		nodes := map[registry.ID]*Node{
			fooID: &Node{ID: fooID},
		}
		err := opts.Validate(nodes)
		assert.Error(t, err)
		assert.Equal(t, "ID :foo is not allowed in this build", err.Error())
	})

	t.Run("same ID in required and denied", func(t *testing.T) {
		opts := NewBuildOptions().
			WithMode(AllowAll).
			WithRequired(fooID).
			WithDenied(fooID)

		nodes := map[registry.ID]*Node{
			fooID: &Node{ID: fooID},
		}
		err := opts.Validate(nodes)
		assert.Error(t, err)
		assert.Equal(t, "ID :foo is not allowed in this build", err.Error())
	})

	t.Run("same ID added multiple times to lists", func(t *testing.T) {
		opts := NewBuildOptions()

		// Add same ID multiple times to allowed list
		opts.WithAllowed(fooID, fooID)
		assert.True(t, opts.Allowed[fooID])

		// Add same ID multiple times to denied list
		opts.WithDenied(fooID, fooID)
		assert.True(t, opts.Denied[fooID])

		// Add same ID multiple times to required list
		opts.WithRequired(fooID, fooID)
		assert.True(t, opts.Required[fooID])
	})
}

func TestBuildOptions_ModificationAfterSetup(t *testing.T) {
	fooID := registry.ID{Name: "foo"}
	barID := registry.ID{Name: "bar"}

	opts := NewBuildOptions().
		WithMode(AllowListed).
		WithAllowed(fooID)

	// Initial validation
	nodes := map[registry.ID]*Node{
		fooID: &Node{ID: fooID},
	}
	assert.NoError(t, opts.Validate(nodes))

	// Add new allowed ID and test
	opts.WithAllowed(barID)
	nodes[barID] = &Node{ID: barID}
	assert.NoError(t, opts.Validate(nodes))

	// Add denied ID and test
	opts.WithDenied(fooID)
	err := opts.Validate(nodes)
	assert.Error(t, err)
	assert.Equal(t, "ID :foo is not allowed in this build", err.Error())
}
