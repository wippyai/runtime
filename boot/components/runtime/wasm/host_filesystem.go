// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	wippyfs "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/filesystem"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var filesystemAliases = []string{
	wippyfs.TypesNamespace,
	wippyfs.FilesystemPreopensNamespace,
	"wasi:filesystem/types",
	"wasi:filesystem/preopens",
}

func filesystemHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippyfs.NewTypesHost(resources),
		wippyfs.NewPreopensHost(resources),
	}
}
