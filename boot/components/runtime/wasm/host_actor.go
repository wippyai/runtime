// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"

	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func actorHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{actor.NewHost(resources)}
}

func actorHostProfile() wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{actorHosts}, nil, "wippy:actor")
		},
	}
}
