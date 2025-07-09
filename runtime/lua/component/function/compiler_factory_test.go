package function

import (
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewCompilerFactory(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{} // Use concrete type
	id := registry.ID{Name: "test"}
	buildOpts := code.NewBuildOptions()
	imports := []code.Import{
		{ID: registry.ID{Name: "test_module"}, Alias: "test"},
	}
	options := component.WithRunnerOption()

	factory := NewCompilerFactory(log, codeManager, id, buildOpts, imports, options)

	assert.NotNil(t, factory)
	assert.Equal(t, log, factory.log)
	assert.Equal(t, codeManager, factory.code)
	assert.Equal(t, id, factory.id)
	assert.Equal(t, buildOpts, factory.buildOpts)
	assert.Equal(t, imports, factory.imports)
	// Don't compare function types directly
	assert.NotNil(t, factory.options)
}

func TestFactory_Compile(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{} // Use concrete type
	id := registry.ID{Name: "test"}
	buildOpts := code.NewBuildOptions()
	imports := []code.Import{}
	options := component.WithRunnerOption()

	factory := NewCompilerFactory(log, codeManager, id, buildOpts, imports, options)

	// Compile should be a no-op
	err := factory.Compile()

	assert.NoError(t, err)
}
