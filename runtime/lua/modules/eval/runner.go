package eval

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
)

func buildIsolatedRunner(ctx context.Context, config *evalConfig) (*engine.Runner, error) {
	cmInterface := luaapi.GetCodeManager(ctx)
	if cmInterface == nil {
		return nil, fmt.Errorf("code manager not found in context")
	}

	cm, ok := cmInterface.(*code.Manager)
	if !ok {
		return nil, fmt.Errorf("invalid code manager type")
	}

	options := make([]engine.Option, 0, len(config.Modules)+len(config.Imports))

	for _, modName := range config.Modules {
		id := registry.ID{NS: "", Name: modName}
		node, err := cm.GetNode(id)
		if err != nil {
			return nil, fmt.Errorf("module '%s' not found: %w", modName, err)
		}

		if node.Kind != luaapi.KindModule {
			return nil, fmt.Errorf("'%s' is not a module (kind: %s)", modName, node.Kind)
		}

		if node.Module == nil {
			return nil, fmt.Errorf("module '%s' has no loader", modName)
		}

		options = append(options, engine.WithLoader(node.Module.Name(), node.Module.Loader))
	}

	for alias, importID := range config.Imports {
		id := registry.ParseID(importID)

		node, err := cm.GetNode(id)
		if err != nil {
			return nil, fmt.Errorf("import '%s' not found: %w", importID, err)
		}

		if node.Kind != luaapi.KindLibrary {
			return nil, fmt.Errorf("'%s' is not a library (kind: %s)", importID, node.Kind)
		}

		if node.Source == "" {
			return nil, fmt.Errorf("library '%s' has no source", importID)
		}

		options = append(options, engine.WithLibrary(alias, node.Source))
	}

	cvm, err := engine.NewCVM(nil, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CVM: %w", err)
	}

	if err := cvm.Import(config.Source, "eval", config.Method); err != nil {
		cvm.Close()
		return nil, fmt.Errorf("failed to import source: %w", err)
	}

	runner := engine.NewRunner(cvm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))
	return runner, nil
}
