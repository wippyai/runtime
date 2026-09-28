// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"os"

	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/historybinding"
	"github.com/wippyai/runtime/runtime/security"
	"github.com/wippyai/runtime/system/registry/history/composite"
	"github.com/wippyai/runtime/system/registry/history/remote"
)

// registryHistoryBackend returns the active history backend.
func registryHistoryBackend(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushBackendError(l, lua.NewLuaError(l, "no context").WithKind(lua.Internal))
	}
	if !security.IsAllowed(ctx, "registry.history.get", "", nil) {
		return pushBackendError(l, lua.NewLuaError(l, "registry history read is not allowed").WithKind(lua.PermissionDenied))
	}
	table := lua.CreateTable(0, 4)
	table.RawSetString("backend", lua.LString(historybinding.BackendLocal))
	if reg := regapi.GetRegistry(ctx); reg != nil {
		if selected, ok := reg.History().(*composite.History); ok {
			if active, ok := selected.Active().(*remote.History); ok {
				key := active.Key()
				table.RawSetString("backend", lua.LString(historybinding.BackendRemote))
				table.RawSetString("name", lua.LString(key.GetRegistryId()))
				table.RawSetString("organization_id", lua.LString(key.GetTenantId()))
				table.RawSetString("environment", lua.LString(key.GetEnvironmentId()))
			}
		}
	}
	l.Push(table)
	l.Push(lua.LNil)
	return 2
}

// registryUseRemoteHistory transfers the local history to a remote history
// and switches the registry to it.
func registryUseRemoteHistory(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushBackendError(l, lua.NewLuaError(l, "no context").WithKind(lua.Internal))
	}
	options := l.CheckTable(1)
	settings := historybinding.Settings{
		RegistryID:    optionString(options, "name"),
		Organization:  optionString(options, "organization"),
		TenantID:      optionString(options, "organization_id"),
		EnvironmentID: optionString(options, "environment"),
		Endpoint:      optionString(options, "endpoint"),
		Timeout:       historybinding.DefaultTimeout,
	}
	if settings.RegistryID == "" {
		return pushBackendError(l, lua.NewLuaError(l, "remote history name is required").WithKind(lua.Invalid))
	}
	if !security.IsAllowed(ctx, "registry.history.select", settings.RegistryID, nil) {
		return pushBackendError(l, lua.NewLuaError(l, "registry history selection is not allowed").WithKind(lua.PermissionDenied))
	}
	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		return pushBackendError(l, lua.NewLuaError(l, "registry not found in context").WithKind(lua.Internal))
	}
	history, ok := reg.History().(*composite.History)
	if !ok {
		return pushBackendError(l, lua.NewLuaError(l, "registry history cannot be switched").WithKind(lua.Invalid))
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return pushBackendError(l, lua.WrapErrorWithLua(l, err, "resolve project directory").WithKind(lua.Internal))
	}
	binding, err := historybinding.UseRemote(ctx, history, projectDir, settings)
	if err != nil {
		return pushBackendError(l, lua.WrapErrorWithLua(l, err, "use remote history").WithKind(lua.Internal))
	}
	table := lua.CreateTable(0, 4)
	table.RawSetString("backend", lua.LString(binding.Backend))
	table.RawSetString("name", lua.LString(binding.RegistryID))
	table.RawSetString("organization_id", lua.LString(binding.TenantID))
	table.RawSetString("environment", lua.LString(binding.EnvironmentID))
	l.Push(table)
	l.Push(lua.LNil)
	return 2
}

func optionString(options *lua.LTable, key string) string {
	if value, ok := options.RawGetString(key).(lua.LString); ok {
		return string(value)
	}
	return ""
}

func pushBackendError(l *lua.LState, err *lua.Error) int {
	l.Push(lua.LNil)
	l.Push(err.WithRetryable(false))
	return 2
}
