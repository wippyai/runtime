// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	sysregistry "github.com/wippyai/runtime/system/registry"
)

type installedEntryTestRegistry struct {
	readErr error
	*mockRegistry
	installed regapi.InstalledEntry
}

func (r *installedEntryTestRegistry) GetInstalledEntry(id regapi.ID, expected string) (regapi.InstalledEntry, error) {
	if r.readErr != nil {
		return regapi.InstalledEntry{}, r.readErr
	}
	if expected != "" && expected != r.installed.Revision {
		return regapi.InstalledEntry{}, sysregistry.ErrInstalledEntryRevisionChanged
	}
	if !id.Equal(r.installed.Entry.ID) {
		return regapi.InstalledEntry{}, sysregistry.NewEntryNotFoundError(id)
	}
	return r.installed, nil
}

func TestRegistryGetInstalledEntryReturnsNativeOwnershipWithReadPermission(t *testing.T) {
	lua.SetErrorMetadataExtractor(runtimelua.ErrorMetadata)
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })
	ctx, closeFrame := strictOverlayContext(t, "registry.get\x00app:visible", "registry.get\x00app:missing")
	defer closeFrame()
	reg := &installedEntryTestRegistry{mockRegistry: &mockRegistry{}, installed: regapi.InstalledEntry{
		Entry: regapi.Entry{
			ID:       regapi.NewID("app", "visible"),
			Kind:     regapi.EntryKind,
			Meta:     attrs.NewBagFrom(map[string]any{"owner": "caller/claim"}),
			Registry: regapi.EntryMetadata{Owner: "installed/package"},
		},
		Revision: "opaque-revision",
		Version:  42,
	}}
	l := lua.NewState()
	defer l.Close()
	l.SetContext(regapi.WithRegistry(ctx, reg))
	lua.OpenErrors(l)
	setupModule(l)

	require.NoError(t, l.DoString(`
		local checked = assert(registry.get_installed_entry("app:visible"))
		assert(checked.version == 42)
		assert(checked.revision == "opaque-revision")
		assert(checked.entry.id == "app:visible")
		assert(checked.entry.meta.owner == "caller/claim")
		assert(checked.entry.registry.owner == "installed/package")
		checked.entry.registry.owner = "caller/claim"
		local again = assert(registry.get_installed_entry("app:visible", checked.revision))
		assert(again.entry.registry.owner == "installed/package")

		local missing, err = registry.get_installed_entry("app:missing")
		assert(missing == nil and err:kind() == errors.NOT_FOUND)
		local denied, err = registry.get_installed_entry("app:private", checked.revision)
		assert(denied == nil and err:kind() == errors.PERMISSION_DENIED)
		local invalid, err = registry.get_installed_entry("app:visible", "")
		assert(invalid == nil and err:kind() == errors.INVALID)
		local stale, err = registry.get_installed_entry("app:visible", "wrong-revision")
		assert(stale == nil and err:kind() == errors.CONFLICT and err:retryable())
	`))

	reg.readErr = sysregistry.ErrInstalledStateUnavailable
	require.NoError(t, l.DoString(`
		local entry, err = registry.get_installed_entry("app:visible")
		assert(entry == nil and err:kind() == errors.UNAVAILABLE and err:retryable())
	`))
}
