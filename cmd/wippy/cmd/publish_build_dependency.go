// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/semver"
	"github.com/wippyai/runtime/boot/deps/lock"
)

type frontendProvenance struct {
	Imports         []frontendImportProvenance `json:"imports"`
	ManifestVersion int                        `json:"manifest_version"`
}

type frontendImportProvenance struct {
	Entry   string `json:"entry"`
	Module  string `json:"module"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

func stripBuildDependencies(
	ctx context.Context,
	transcoder payload.Transcoder,
	entries []regapi.Entry,
	lockObj *lock.Lock,
) ([]regapi.Entry, frontendProvenance, error) {
	provenance := frontendProvenance{ManifestVersion: 1}
	filtered := make([]regapi.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != regapi.NamespaceBuildDependency {
			filtered = append(filtered, entry)
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, frontendProvenance{}, err
		}
		var definition struct {
			Component  string `json:"component"`
			Version    string `json:"version"`
			Parameters []any  `json:"parameters"`
		}
		if err := transcoder.Unmarshal(entry.Data, &definition); err != nil {
			return nil, frontendProvenance{}, fmt.Errorf("decode build dependency %s: %w", entry.ID.String(), err)
		}
		if definition.Component == "" {
			return nil, frontendProvenance{}, fmt.Errorf("build dependency %s requires component", entry.ID.String())
		}
		if len(definition.Parameters) > 0 {
			return nil, frontendProvenance{}, fmt.Errorf("build dependency %s cannot declare parameters", entry.ID.String())
		}
		module, ok := lockObj.GetModule(definition.Component)
		if !ok || module.Version == "" || module.Hash == "" {
			return nil, frontendProvenance{}, fmt.Errorf("build dependency %s is not pinned with a digest", definition.Component)
		}
		declaredVersion, err := semver.ParseVersion(definition.Version)
		if err != nil {
			return nil, frontendProvenance{}, fmt.Errorf("build dependency %s must use an exact semver version: %s", entry.ID.String(), definition.Version)
		}
		lockedVersion, err := semver.ParseVersion(module.Version)
		if err != nil || !declaredVersion.Equal(lockedVersion) {
			return nil, frontendProvenance{}, fmt.Errorf("build dependency %s requires %s@%s but lock selects %s", entry.ID.String(), definition.Component, definition.Version, module.Version)
		}
		provenance.Imports = append(provenance.Imports, frontendImportProvenance{
			Entry:   entry.ID.String(),
			Module:  definition.Component,
			Version: module.Version,
			Digest:  module.Hash,
		})
	}
	sort.Slice(provenance.Imports, func(i, j int) bool {
		left, right := provenance.Imports[i], provenance.Imports[j]
		if left.Module != right.Module {
			return left.Module < right.Module
		}
		return left.Entry < right.Entry
	})
	return filtered, provenance, nil
}
