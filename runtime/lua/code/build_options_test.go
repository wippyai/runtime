package lua

import (
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestBuildOptions_validateModules(t *testing.T) {
	tests := []struct {
		name      string
		opts      *BuildOptions
		modules   []runtime.Module
		wantError bool
		errorMsg  string
	}{
		{
			name: "AllowAll mode - allow everything not denied",
			opts: NewBuildOptions().
				WithAccessMode(AllowAll),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: false,
		},
		{
			name: "AllowAll mode with denied module",
			opts: NewBuildOptions().
				WithAccessMode(AllowAll).
				WithDeniedModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: true,
			errorMsg:  "module foo is not allowed in this build",
		},
		{
			name: "AllowListed mode - only allow listed",
			opts: NewBuildOptions().
				WithAccessMode(AllowListed).
				WithAllowedModules("foo", "bar"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: false,
		},
		{
			name: "AllowListed mode - reject unlisted",
			opts: NewBuildOptions().
				WithAccessMode(AllowListed).
				WithAllowedModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: true,
			errorMsg:  "module bar is not in the allowed modules list",
		},
		{
			name: "DenyAll mode - only allow required",
			opts: NewBuildOptions().
				WithAccessMode(DenyAll).
				WithRequiredModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
			},
			wantError: false,
		},
		{
			name: "DenyAll mode - reject non-required",
			opts: NewBuildOptions().
				WithAccessMode(DenyAll).
				WithRequiredModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: true,
			errorMsg:  "module bar is not allowed (DenyAll mode)",
		},
		{
			name: "StrictListed mode - required must be allowed",
			opts: NewBuildOptions().
				WithAccessMode(StrictListed).
				WithAllowedModules("foo", "bar").
				WithRequiredModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: false,
		},
		{
			name: "StrictListed mode - fail if required not allowed",
			opts: NewBuildOptions().
				WithAccessMode(StrictListed).
				WithAllowedModules("bar").
				WithRequiredModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: true,
			errorMsg:  "required module foo must also be in allowed list (StrictListed mode)",
		},
		{
			name: "Missing required module",
			opts: NewBuildOptions().
				WithAccessMode(AllowAll).
				WithRequiredModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "bar"},
			},
			wantError: true,
			errorMsg:  "required module foo was not found",
		},
		{
			name: "Denied takes precedence over required",
			opts: NewBuildOptions().
				WithAccessMode(AllowAll).
				WithRequiredModules("foo").
				WithDeniedModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
			},
			wantError: true,
			errorMsg:  "module foo is not allowed in this build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validateModules(tt.modules)
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

	// Test WithAccessMode
	opts.WithAccessMode(AllowListed)
	assert.Equal(t, AllowListed, opts.AccessMode)

	// Test WithAllowedModules
	opts.WithAllowedModules("foo", "bar")
	assert.True(t, opts.AllowedModules["foo"])
	assert.True(t, opts.AllowedModules["bar"])
	assert.False(t, opts.AllowedModules["baz"])

	// Test WithDeniedModules
	opts.WithDeniedModules("baz", "qux")
	assert.True(t, opts.DeniedModules["baz"])
	assert.True(t, opts.DeniedModules["qux"])
	assert.False(t, opts.DeniedModules["foo"])

	// Test WithRequiredModules
	opts.WithRequiredModules("req1", "req2")
	assert.True(t, opts.RequiredModules["req1"])
	assert.True(t, opts.RequiredModules["req2"])
	assert.False(t, opts.RequiredModules["foo"])

	// Test chaining
	opts2 := NewBuildOptions().
		WithAccessMode(DenyAll).
		WithRequiredModules("foo").
		WithAllowedModules("bar")

	assert.Equal(t, DenyAll, opts2.AccessMode)
	assert.True(t, opts2.RequiredModules["foo"])
	assert.True(t, opts2.AllowedModules["bar"])
}

func TestBuildOptions_EmptyModules(t *testing.T) {
	tests := []struct {
		name      string
		opts      *BuildOptions
		wantError bool
		errorMsg  string
	}{
		{
			name: "empty modules with no requirements",
			opts: NewBuildOptions().
				WithAccessMode(AllowAll),
			wantError: false,
		},
		{
			name: "empty modules with required modules",
			opts: NewBuildOptions().
				WithAccessMode(AllowAll).
				WithRequiredModules("foo"),
			wantError: true,
			errorMsg:  "required module foo was not found",
		},
		{
			name: "empty modules in DenyAll mode",
			opts: NewBuildOptions().
				WithAccessMode(DenyAll),
			wantError: false,
		},
		{
			name: "empty modules in StrictListed mode",
			opts: NewBuildOptions().
				WithAccessMode(StrictListed).
				WithAllowedModules("foo").
				WithRequiredModules("foo"),
			wantError: true,
			errorMsg:  "required module foo was not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validateModules([]runtime.Module{})
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

func TestBuildOptions_ModuleNameEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		opts      *BuildOptions
		modules   []runtime.Module
		wantError bool
		errorMsg  string
	}{
		{
			name: "empty string module name",
			opts: NewBuildOptions().
				WithAccessMode(AllowListed).
				WithAllowedModules(""),
			modules: []runtime.Module{
				&dummyModule{name: ""},
			},
			wantError: false,
		},
		{
			name: "duplicate module names",
			opts: NewBuildOptions().
				WithAccessMode(AllowListed).
				WithAllowedModules("foo"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "foo"},
			},
			wantError: false,
		},
		{
			name: "case sensitivity check",
			opts: NewBuildOptions().
				WithAccessMode(AllowListed).
				WithAllowedModules("Foo", "BAR"),
			modules: []runtime.Module{
				&dummyModule{name: "foo"},
				&dummyModule{name: "bar"},
			},
			wantError: true,
			errorMsg:  "module foo is not in the allowed modules list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validateModules(tt.modules)
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

func TestBuildOptions_MultipleRequiredModules(t *testing.T) {
	opts := NewBuildOptions().
		WithAccessMode(AllowAll).
		WithRequiredModules("foo", "bar", "baz")

	// Test all required modules present
	modules := []runtime.Module{
		&dummyModule{name: "foo"},
		&dummyModule{name: "bar"},
		&dummyModule{name: "baz"},
		&dummyModule{name: "extra"},
	}
	assert.NoError(t, opts.validateModules(modules))

	// Test missing one required module
	modules = []runtime.Module{
		&dummyModule{name: "foo"},
		&dummyModule{name: "bar"},
		&dummyModule{name: "extra"},
	}
	err := opts.validateModules(modules)
	assert.Error(t, err)
	assert.Equal(t, "required module baz was not found", err.Error())

	// Test missing all required modules
	modules = []runtime.Module{
		&dummyModule{name: "extra1"},
		&dummyModule{name: "extra2"},
	}
	err = opts.validateModules(modules)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required module")
}

func TestBuildOptions_StateConsistency(t *testing.T) {
	t.Run("same module in allowed and denied", func(t *testing.T) {
		opts := NewBuildOptions().
			WithAccessMode(AllowListed).
			WithAllowedModules("foo").
			WithDeniedModules("foo")

		modules := []runtime.Module{
			&dummyModule{name: "foo"},
		}
		err := opts.validateModules(modules)
		assert.Error(t, err)
		assert.Equal(t, "module foo is not allowed in this build", err.Error())
	})

	t.Run("same module in required and denied", func(t *testing.T) {
		opts := NewBuildOptions().
			WithAccessMode(AllowAll).
			WithRequiredModules("foo").
			WithDeniedModules("foo")

		modules := []runtime.Module{
			&dummyModule{name: "foo"},
		}
		err := opts.validateModules(modules)
		assert.Error(t, err)
		assert.Equal(t, "module foo is not allowed in this build", err.Error())
	})

	t.Run("same module added multiple times to lists", func(t *testing.T) {
		opts := NewBuildOptions()

		// Add same module multiple times to allowed list
		opts.WithAllowedModules("foo", "foo")
		assert.True(t, opts.AllowedModules["foo"])

		// Add same module multiple times to denied list
		opts.WithDeniedModules("bar", "bar")
		assert.True(t, opts.DeniedModules["bar"])

		// Add same module multiple times to required list
		opts.WithRequiredModules("baz", "baz")
		assert.True(t, opts.RequiredModules["baz"])
	})
}

func TestBuildOptions_ModificationAfterSetup(t *testing.T) {
	opts := NewBuildOptions().
		WithAccessMode(AllowListed).
		WithAllowedModules("foo")

	// Initial validation
	modules := []runtime.Module{
		&dummyModule{name: "foo"},
	}
	assert.NoError(t, opts.validateModules(modules))

	// Add new allowed module and test
	opts.WithAllowedModules("bar")
	modules = append(modules, &dummyModule{name: "bar"})
	assert.NoError(t, opts.validateModules(modules))

	// Add denied module and test
	opts.WithDeniedModules("foo")
	err := opts.validateModules(modules)
	assert.Error(t, err)
	assert.Equal(t, "module foo is not allowed in this build", err.Error())
}
