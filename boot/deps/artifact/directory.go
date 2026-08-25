// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	"github.com/wippyai/wapp"
)

// DirectoryResources resolves artifact declarations from selected local
// fs.directory entries. Ownership comes from the state's provenance, which is
// total over its entries. moduleRoots is the authoritative source root for each
// local module; entries outside that set are ignored.
func DirectoryResources(
	ctx context.Context,
	state regapi.ProvenancedState,
	moduleRoots map[string]string,
	moduleVersions map[string]string,
) ([]Resource, error) {
	if len(moduleRoots) == 0 {
		return nil, nil
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return nil, errors.New("payload transcoder is unavailable")
	}

	resources := make([]Resource, 0)
	for _, entry := range state.Entries {
		record, known := state.Prov[entry.ID]
		if !known {
			return nil, regapi.NewMissingProvenanceError(entry.ID)
		}
		module := record.Module
		moduleRoot, selected := moduleRoots[module]
		if !selected || entry.Kind != dirapi.Kind {
			continue
		}
		_, declared, err := ParseDeclaration(wapp.Metadata(entry.Meta))
		if err != nil {
			return nil, fmt.Errorf("local resource %s: %w", entry.ID.String(), err)
		}
		if !declared {
			continue
		}

		var cfg dirapi.Config
		if err := transcoder.Unmarshal(entry.Data, &cfg); err != nil {
			return nil, fmt.Errorf("decode local resource %s: %w", entry.ID.String(), err)
		}
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("validate local resource %s: %w", entry.ID.String(), err)
		}
		directory := cfg.Directory
		if !dirapi.IsConfiguredPathAbsolute(directory) && cfg.Base != dirapi.BaseProject {
			directory = filepath.Join(moduleRoot, directory)
		}
		info, err := os.Stat(directory)
		if err != nil {
			return nil, fmt.Errorf("inspect local resource %s: %w", entry.ID.String(), err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("local resource %s is not a directory", entry.ID.String())
		}
		resources = append(resources, Resource{
			Filesystem:    os.DirFS(directory),
			Meta:          wapp.Metadata(entry.Meta),
			ModuleVersion: moduleVersions[module],
			ResourceID:    wapp.NewID(entry.ID.NS, entry.ID.Name),
			Source:        directory,
		})
	}
	return resources, nil
}
