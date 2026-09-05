// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"

	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

func actorHostProfile() wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register:      func(_ context.Context, rt *wasmrt.Runtime) error { return rt.RegisterHost(actor.NewHost()) },
	}
}
