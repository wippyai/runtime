// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	"sort"

	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	// FilesystemPreopensNamespace exposes WASI preview2 filesystem preopens.
	FilesystemPreopensNamespace = "wasi:filesystem/preopens@0.2.3"
)

// PreopensHost maps invocation-scoped WASI mounts to descriptor resources.
type PreopensHost struct {
	resources *preview2.ResourceTable
}

// NewPreopensHost builds a WASI filesystem preopens host.
func NewPreopensHost(resources *preview2.ResourceTable) *PreopensHost {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &PreopensHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *PreopensHost) Namespace() string {
	return FilesystemPreopensNamespace
}

// GetDirectories returns preopened guest directories for this invocation.
func (h *PreopensHost) GetDirectories(ctx context.Context) [][2]interface{} {
	cfg := wippyhost.GetWASICallConfig(ctx)
	if cfg == nil || len(cfg.Mounts) == 0 {
		return nil
	}

	mounts := make([]wippyhost.WASIMountBinding, len(cfg.Mounts))
	copy(mounts, cfg.Mounts)
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].Guest < mounts[j].Guest
	})

	out := make([][2]interface{}, 0, len(mounts))
	for _, m := range mounts {
		if m.Guest == "" || m.Filesystem == nil {
			continue
		}
		desc := newDescriptorResource(m.Filesystem, ".", true, m.ReadOnly)
		handle := h.resources.Add(desc)
		out = append(out, [2]interface{}{handle, m.Guest})
	}
	return out
}
