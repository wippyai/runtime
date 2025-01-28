package manager

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockTerminalFactory implements TerminalFactory for testing
type mockTerminalFactory struct {
	makeTerminalFunc func(log *zap.Logger, app *api.TerminalConfig, modules api.ModuleRegistry, libraries api.LibraryRegistry) (terminal.Terminal, error)
}

func (m *mockTerminalFactory) MakeTerminal(
	log *zap.Logger,
	app *api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	if m.makeTerminalFunc != nil {
		return m.makeTerminalFunc(log, app, modules, libraries)
	}
	return &mockTerminal{}, nil
}

// mockTerminal implements terminal.Terminal for testing
type mockTerminal struct {
	closed bool
}

func (m *mockTerminal) Run(_ context.Context, _ io.Reader, _ io.Writer) error {
	return nil
}

func (m *mockTerminal) Close(context.Context) error {
	m.closed = true
	return nil
}

func setupTestManagers(t *testing.T) (*Terminals, *Modules, *Libraries) {
	logger := zap.NewNop()
	factory := &mockTerminalFactory{}

	modules := NewModules(logger)
	libraries := NewLibraries(logger)
	terminals := NewTerminals(logger, factory)

	// Register test module
	module := &mockModule{name: "test_module"}
	err := modules.Register(module)
	require.NoError(t, err)

	// Add test library
	libCfg := &api.LibraryConfig{
		Source:  "return {test = function() return 'hello' end}",
		Meta:    registry.Metadata{"name": "test_lib"},
		Modules: []string{"test_module"},
	}
	err = libraries.Add("test_lib", libCfg)
	require.NoError(t, err)

	return terminals, modules, libraries
}

func TestNewTerminals(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockTerminalFactory{}

	t.Run("creates new instance", func(t *testing.T) {
		terminals := NewTerminals(logger, factory)
		assert.NotNil(t, terminals)
		assert.NotNil(t, terminals.terminals)
		assert.Empty(t, terminals.terminals)
		assert.Equal(t, factory, terminals.factory)
	})
}

func TestTerminals_Add(t *testing.T) {
	terminals, modules, libraries := setupTestManagers(t)

	t.Run("adds new terminal successfully", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		err := terminals.Add("test_terminal", cfg, modules, libraries)
		require.NoError(t, err)

		// Verify terminal was stored
		stored, exists := terminals.GetTerminal("test_terminal")
		assert.True(t, exists)
		assert.Equal(t, cfg, stored)
	})

	t.Run("fails adding duplicate terminal", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		err := terminals.Add("test_terminal", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("fails with missing module dependency", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"non_existent_module"},
			Meta:      registry.Metadata{},
		}

		err := terminals.Add("new_terminal", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module non_existent_module not found")
	})

	t.Run("fails with missing library dependency", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"non_existent_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		err := terminals.Add("new_terminal", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "library non_existent_lib not found")
	})
}

func TestTerminals_Update(t *testing.T) {
	terminals, modules, libraries := setupTestManagers(t)

	// First add a terminal
	initialCfg := &api.TerminalConfig{
		Source:    "function init() return 'hello' end",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
	}
	err := terminals.Add("test_terminal", initialCfg, modules, libraries)
	require.NoError(t, err)

	t.Run("updates existing terminal", func(t *testing.T) {
		updatedCfg := &api.TerminalConfig{
			Source:    "function init() return 'updated' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{"version": "2"},
		}

		err := terminals.Update("test_terminal", updatedCfg, modules, libraries)
		require.NoError(t, err)

		// Verify terminal was updated
		stored, exists := terminals.GetTerminal("test_terminal")
		assert.True(t, exists)
		assert.Equal(t, updatedCfg, stored)
	})

	t.Run("fails updating non-existent terminal", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		err := terminals.Update("non_existent", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTerminals_Delete(t *testing.T) {
	terminals, modules, libraries := setupTestManagers(t)

	// First add a terminal
	cfg := &api.TerminalConfig{
		Source:    "function init() return 'hello' end",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
	}
	err := terminals.Add("test_terminal", cfg, modules, libraries)
	require.NoError(t, err)

	t.Run("deletes existing terminal", func(t *testing.T) {
		err := terminals.Delete("test_terminal")
		require.NoError(t, err)

		// Verify terminal was deleted
		_, exists := terminals.GetTerminal("test_terminal")
		assert.False(t, exists)
	})

	t.Run("fails deleting non-existent terminal", func(t *testing.T) {
		err := terminals.Delete("non_existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTerminals_FindDependentOnLibrary(t *testing.T) {
	terminals, modules, libraries := setupTestManagers(t)

	// Add terminals with different dependencies
	cfg1 := &api.TerminalConfig{
		Source:    "function init() return 'hello' end",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
	}
	cfg2 := &api.TerminalConfig{
		Source:    "function init() return 'world' end",
		Libraries: []string{},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
	}

	err := terminals.Add("terminal1", cfg1, modules, libraries)
	require.NoError(t, err)
	err = terminals.Add("terminal2", cfg2, modules, libraries)
	require.NoError(t, err)

	t.Run("finds dependent terminals", func(t *testing.T) {
		dependent := terminals.FindDependentOnLibrary("test_lib")
		assert.Len(t, dependent, 1)
		terminal, exists := dependent["terminal1"]
		assert.True(t, exists)
		assert.Equal(t, cfg1, terminal)
	})

	t.Run("returns empty map for no dependents", func(t *testing.T) {
		dependent := terminals.FindDependentOnLibrary("non_existent_lib")
		assert.Empty(t, dependent)
	})
}

func TestTerminals_MakeTerminal(t *testing.T) {
	terminals, modules, libraries := setupTestManagers(t)

	t.Run("creates terminal successfully", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		term, err := terminals.MakeTerminal("test_terminal", cfg, modules, libraries)
		require.NoError(t, err)
		assert.NotNil(t, term)
	})

	t.Run("fails with invalid dependencies", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"non_existent_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		term, err := terminals.MakeTerminal("test_terminal", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Nil(t, term)
		assert.Contains(t, err.Error(), "library non_existent_lib not found")
	})

	t.Run("factory error is propagated", func(t *testing.T) {
		factoryErr := fmt.Errorf("factory error")
		terminals.factory = &mockTerminalFactory{
			makeTerminalFunc: func(_ *zap.Logger, _ *api.TerminalConfig, _ api.ModuleRegistry, _ api.LibraryRegistry) (terminal.Terminal, error) {
				return nil, factoryErr
			},
		}

		cfg := &api.TerminalConfig{
			Source:    "function init() return 'hello' end",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
		}

		term, err := terminals.MakeTerminal("test_terminal", cfg, modules, libraries)
		assert.Error(t, err)
		assert.Nil(t, term)
		assert.Equal(t, factoryErr, err)
	})
}
