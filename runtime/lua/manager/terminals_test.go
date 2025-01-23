package manager

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"io"
	"testing"
)

// mockTerminalFactory implements TerminalFactory for testing
type mockTerminalFactory struct {
	makeTerminalFunc func(*zap.Logger, api.TerminalConfig, api.ModuleRegistry, api.LibraryRegistry) (terminal.Terminal, error)
}

func (f *mockTerminalFactory) MakeTerminal(
	log *zap.Logger,
	cfg api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	if f.makeTerminalFunc != nil {
		return f.makeTerminalFunc(log, cfg, modules, libraries)
	}
	return &mockTerminal{}, nil
}

type mockTerminal struct{}

func (t *mockTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	return nil
}

func (t *mockTerminal) Close(ctx context.Context) error {
	return nil
}

func makeTestTerminalEntry(id string, cfg *api.TerminalConfig) registry.Entry {
	return registry.Entry{
		ID:   registry.ID(id),
		Kind: api.KindTerminal,
		Meta: registry.Metadata{},
		Data: payload.NewPayload(cfg, payload.Golang),
	}
}

// Helper to create default lifecycle config
func makeDefaultLifecycleConfig() supervisor.LifecycleConfig {
	cfg := supervisor.LifecycleConfig{
		AutoStart: true,
		RetryPolicy: supervisor.RetryPolicy{
			BackoffFactor: 2.0,
			Jitter:        0.1,
			MaxAttempts:   5,
		},
	}
	cfg.InitDefaults()
	return cfg
}

func TestNewTerminals(t *testing.T) {
	logger := zap.NewNop()
	dtt := makeTestTranscoder()
	factory := &mockTerminalFactory{}

	t.Run("creates new instance", func(t *testing.T) {
		terms := NewTerminals(logger, dtt, factory)
		assert.NotNil(t, terms)
		assert.NotNil(t, terms.terminals)
		assert.Empty(t, terms.terminals)
	})
}

func setupTerminalManagers(t *testing.T) (*Terminals, *Modules, *Libraries) {
	logger := zap.NewNop()
	dtt := makeTestTranscoder()
	factory := &mockTerminalFactory{}

	modules := NewModules(logger)
	libraries := NewLibraries(dtt, logger)
	terminals := NewTerminals(logger, dtt, factory)

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
	err = libraries.Add(context.Background(), makeTestEntry("test_lib", libCfg))
	require.NoError(t, err)

	return terminals, modules, libraries
}

func TestTerminals_Add(t *testing.T) {
	terminals, modules, libraries := setupTerminalManagers(t)

	t.Run("adds new terminal successfully", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function main() print('hello') end",
			Method:    "main",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
			Options:   terminal.Options{},
			Lifecycle: makeDefaultLifecycleConfig(),
		}
		err := terminals.Add(makeTestTerminalEntry("test_term", cfg), modules, libraries)
		require.NoError(t, err)

		// Verify terminal was stored
		stored, exists := terminals.GetTerminal("test_term")
		assert.True(t, exists)
		assert.Equal(t, cfg, stored)
	})

	t.Run("fails adding duplicate terminal", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function main() print('hello') end",
			Method:    "main",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
			Options:   terminal.Options{},
			Lifecycle: makeDefaultLifecycleConfig(),
		}
		err := terminals.Add(makeTestTerminalEntry("test_term", cfg), modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("fails with missing module dependency", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function main() print('hello') end",
			Method:    "main",
			Libraries: []string{"test_lib"},
			Modules:   []string{"non_existent_module"},
			Meta:      registry.Metadata{},
			Options:   terminal.Options{},
			Lifecycle: makeDefaultLifecycleConfig(),
		}
		err := terminals.Add(makeTestTerminalEntry("new_term", cfg), modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "module non_existent_module not found")
	})

	t.Run("fails with missing library dependency", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function main() print('hello') end",
			Method:    "main",
			Libraries: []string{"non_existent_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
			Options:   terminal.Options{},
			Lifecycle: makeDefaultLifecycleConfig(),
		}
		err := terminals.Add(makeTestTerminalEntry("new_term", cfg), modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "library non_existent_lib not found")
	})
}

func TestTerminals_Update(t *testing.T) {
	terminals, modules, libraries := setupTerminalManagers(t)

	// First add a terminal
	initialCfg := &api.TerminalConfig{
		Source:    "function main() print('hello') end",
		Method:    "main",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
		Options:   terminal.Options{},
		Lifecycle: makeDefaultLifecycleConfig(),
	}
	err := terminals.Add(makeTestTerminalEntry("test_term", initialCfg), modules, libraries)
	require.NoError(t, err)

	t.Run("updates existing terminal", func(t *testing.T) {
		updatedCfg := &api.TerminalConfig{
			Source:    "function main() print('updated') end",
			Method:    "main",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{"version": "2"},
			Options:   terminal.Options{Title: "Updated Terminal"},
			Lifecycle: makeDefaultLifecycleConfig(),
		}
		err := terminals.Update(makeTestTerminalEntry("test_term", updatedCfg), modules, libraries)
		require.NoError(t, err)

		// Verify terminal was updated
		stored, exists := terminals.GetTerminal("test_term")
		assert.True(t, exists)
		assert.Equal(t, updatedCfg, stored)
	})

	t.Run("fails updating non-existent terminal", func(t *testing.T) {
		cfg := &api.TerminalConfig{
			Source:    "function main() print('hello') end",
			Method:    "main",
			Libraries: []string{"test_lib"},
			Modules:   []string{"test_module"},
			Meta:      registry.Metadata{},
			Options:   terminal.Options{},
			Lifecycle: makeDefaultLifecycleConfig(),
		}
		err := terminals.Update(makeTestTerminalEntry("non_existent", cfg), modules, libraries)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTerminals_Delete(t *testing.T) {
	terminals, modules, libraries := setupTerminalManagers(t)

	// First add a terminal
	cfg := &api.TerminalConfig{
		Source:    "function main() print('hello') end",
		Method:    "main",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
		Options:   terminal.Options{},
		Lifecycle: makeDefaultLifecycleConfig(),
	}
	err := terminals.Add(makeTestTerminalEntry("test_term", cfg), modules, libraries)
	require.NoError(t, err)

	t.Run("deletes existing terminal", func(t *testing.T) {
		err := terminals.Delete(makeTestTerminalEntry("test_term", nil))
		require.NoError(t, err)

		// Verify terminal was deleted
		_, exists := terminals.GetTerminal("test_term")
		assert.False(t, exists)
	})

	t.Run("fails deleting non-existent terminal", func(t *testing.T) {
		err := terminals.Delete(makeTestTerminalEntry("non_existent", nil))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTerminals_FindDependentOnLibrary(t *testing.T) {
	terminals, modules, libraries := setupTerminalManagers(t)

	// Add two terminals with different dependencies
	cfg1 := &api.TerminalConfig{
		Source:    "function main() print('hello') end",
		Method:    "main",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
		Options:   terminal.Options{},
		Lifecycle: makeDefaultLifecycleConfig(),
	}
	cfg2 := &api.TerminalConfig{
		Source:    "function main() print('world') end",
		Method:    "main",
		Libraries: []string{},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
		Options:   terminal.Options{},
		Lifecycle: makeDefaultLifecycleConfig(),
	}

	err := terminals.Add(makeTestTerminalEntry("term1", cfg1), modules, libraries)
	require.NoError(t, err)
	err = terminals.Add(makeTestTerminalEntry("term2", cfg2), modules, libraries)
	require.NoError(t, err)

	t.Run("finds dependent terminals", func(t *testing.T) {
		dependent := terminals.FindDependentOnLibrary("test_lib")
		assert.Len(t, dependent, 1)
		assert.Contains(t, dependent, registry.ID("term1"))
	})

	t.Run("returns empty slice for no dependents", func(t *testing.T) {
		dependent := terminals.FindDependentOnLibrary("non_existent_lib")
		assert.Empty(t, dependent)
	})
}

func TestTerminals_MakeTerminal(t *testing.T) {
	terminals, modules, libraries := setupTerminalManagers(t)

	// Add a terminal
	cfg := &api.TerminalConfig{
		Source:    "function main() print('hello') end",
		Method:    "main",
		Libraries: []string{"test_lib"},
		Modules:   []string{"test_module"},
		Meta:      registry.Metadata{},
		Options:   terminal.Options{},
		Lifecycle: makeDefaultLifecycleConfig(),
	}
	err := terminals.Add(makeTestTerminalEntry("test_term", cfg), modules, libraries)
	require.NoError(t, err)

	t.Run("creates terminal successfully", func(t *testing.T) {
		term, err := terminals.MakeTerminal("test_term", modules, libraries)
		require.NoError(t, err)
		assert.NotNil(t, term)

		// Verify we can call the Run method
		err = term.Run(context.Background(), nil, nil)
		assert.NoError(t, err)
	})

	t.Run("fails with non-existent terminal", func(t *testing.T) {
		term, err := terminals.MakeTerminal("non_existent", modules, libraries)
		assert.Error(t, err)
		assert.Nil(t, term)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("fails with factory error", func(t *testing.T) {
		// Create new terminals manager with failing factory
		failingFactory := &mockTerminalFactory{
			makeTerminalFunc: func(*zap.Logger, api.TerminalConfig, api.ModuleRegistry, api.LibraryRegistry) (terminal.Terminal, error) {
				return nil, fmt.Errorf("factory error")
			},
		}
		failingTerminals := NewTerminals(terminals.log, terminals.dtt, failingFactory)

		// Add the same terminal config
		err := failingTerminals.Add(makeTestTerminalEntry("test_term", cfg), modules, libraries)
		require.NoError(t, err)

		// Try to make terminal
		term, err := failingTerminals.MakeTerminal("test_term", modules, libraries)
		assert.Error(t, err)
		assert.Nil(t, term)
		assert.Contains(t, err.Error(), "factory error")
	})
}
