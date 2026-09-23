// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	moduleconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
)

func packNormalizationStages(moduleName string, excludeNS, excludeEntries []string) []boot.Stage {
	var pipelineStages []boot.Stage
	if moduleName == "" {
		pipelineStages = append(pipelineStages, stages.Override())
	}
	if len(excludeNS) > 0 || len(excludeEntries) > 0 {
		pipelineStages = append(pipelineStages, stages.Disable(excludeNS, excludeEntries))
	}
	if moduleName == "" {
		pipelineStages = append(pipelineStages, stages.Disable())
		pipelineStages = append(pipelineStages, stages.Link())
		pipelineStages = append(pipelineStages, stages.Override())
	} else {
		// A reusable module artifact keeps its own requirement defaults, but
		// never bakes in dependency parameters, overrides or disable rules
		// supplied by any assembling deployment.
		pipelineStages = append(pipelineStages, stages.Link(stages.WithDependencies(nil)))
	}
	return pipelineStages
}

// resolvePackModule resolves --module against the effective lock load paths.
// GetModuleLoadPaths has already applied replacements, so a replacement source
// is the only path considered for a replaced module.
func resolvePackModule(moduleName string, modulePaths []lock.ModuleLoadPath, rootModules []string) (lock.ModuleLoadPath, error) {
	if strings.TrimSpace(moduleName) != moduleName || moduleName == "" {
		return lock.ModuleLoadPath{}, fmt.Errorf("--module must be a selected module in org/name format")
	}

	parsed, err := graph.ParseName(moduleName)
	if err != nil {
		return lock.ModuleLoadPath{}, fmt.Errorf("invalid --module %q: %w", moduleName, err)
	}
	moduleName = parsed.String()

	var selected lock.ModuleLoadPath
	found := false
	for _, modulePath := range modulePaths {
		if modulePath.Module != moduleName {
			continue
		}
		if found {
			return lock.ModuleLoadPath{}, fmt.Errorf("selected module %q has multiple effective load paths", moduleName)
		}
		selected = modulePath
		found = true
	}
	if !found {
		for _, rootModule := range rootModules {
			if rootModule != moduleName {
				continue
			}
			for _, modulePath := range modulePaths {
				if modulePath.Module == "" && modulePath.Root {
					// The application source is owned by the selected root module,
					// but the lock loader intentionally leaves its owner empty for
					// deployment composition. Mark it for module-pack selection only.
					modulePath.Module = moduleName
					return modulePath, nil
				}
			}
		}
		return lock.ModuleLoadPath{}, fmt.Errorf("module %q is not selected by the lock file", moduleName)
	}

	return selected, nil
}

func isRootModulePackSource(selected lock.ModuleLoadPath, modulePaths []lock.ModuleLoadPath) bool {
	for _, modulePath := range modulePaths {
		if modulePath.Module == "" && modulePath.Root && modulePath.Path == selected.Path {
			return true
		}
	}
	return false
}

func assignRootModulePackOwner(items []regapi.Entry, moduleName string) {
	for i := range items {
		if items[i].Registry.Owner == "" {
			items[i].Registry.Owner = moduleName
		}
	}
}

// selectModulePackEntries keeps ownership assigned by the lock loader. Entry
// IDs and author metadata are deliberately not used for this boundary because
// package contents cannot claim a different module owner.
func selectModulePackEntries(items []regapi.Entry, moduleName string) ([]regapi.Entry, error) {
	selected := make([]regapi.Entry, 0, len(items))
	for _, item := range items {
		if item.Registry.Owner == moduleName {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("module %q has no entries in the selected lock graph", moduleName)
	}
	return selected, nil
}

func filterModulePackResources(resources []wapp.ResourceSpec, items []regapi.Entry) []wapp.ResourceSpec {
	owned := make(map[wapp.ID]struct{}, len(items))
	for _, item := range items {
		owned[wapp.NewID(item.ID.NS, item.ID.Name)] = struct{}{}
	}

	filtered := make([]wapp.ResourceSpec, 0, len(resources))
	for _, resource := range resources {
		if _, ok := owned[resource.ID]; ok {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func collectEmbeddedPackResourcesForModule(
	modulePaths []lock.ModuleLoadPath,
	existing []wapp.ResourceSpec,
	moduleName string,
) ([]wapp.ResourceSpec, []embeddedPackResourceHandle, error) {
	if moduleName == "" {
		return collectEmbeddedPackResources(modulePaths, existing)
	}

	selected := make([]lock.ModuleLoadPath, 0, 1)
	for _, modulePath := range modulePaths {
		if modulePath.Module == moduleName {
			selected = append(selected, modulePath)
		}
	}
	return collectEmbeddedPackResources(selected, existing)
}

// modulePackMetadata reads the selected module's own publication metadata. A
// source directory uses its wippy.yaml manifest; an existing WAPP preserves
// the archive metadata. A lock-only source without a manifest gets the
// canonical identity from the selected lock module.
func modulePackMetadata(modulePath lock.ModuleLoadPath, moduleName string) (attrs.Bag, error) {
	parsed, err := graph.ParseName(moduleName)
	if err != nil {
		return nil, fmt.Errorf("invalid module %q: %w", moduleName, err)
	}

	metadata := attrs.Bag{
		"name":      parsed.Module,
		"namespace": parsed.Organization + "." + parsed.Module,
		"version":   modulePath.Version,
	}

	if hasWappExtension(modulePath.Path) {
		file, err := os.Open(modulePath.Path)
		if err != nil {
			return nil, fmt.Errorf("open selected module pack %s: %w", modulePath.Path, err)
		}
		defer file.Close()

		reader, err := wapp.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("read selected module pack %s: %w", modulePath.Path, err)
		}
		packMetadata, err := reader.GetMetadata()
		if err != nil {
			return nil, fmt.Errorf("read selected module metadata %s: %w", modulePath.Path, err)
		}
		metadata = attrs.NewBagFrom(packMetadata)
		if err := validateModulePackIdentity(metadata, moduleName); err != nil {
			return nil, err
		}
		if _, ok := metadata["name"]; !ok {
			metadata["name"] = parsed.Module
		}
		if _, ok := metadata["namespace"]; !ok {
			metadata["namespace"] = parsed.Organization + "." + parsed.Module
		}
		if _, ok := metadata["version"]; !ok {
			metadata["version"] = modulePath.Version
		}
		return metadata, nil
	}

	root := modulePath.SourceRoot
	if root == "" {
		root = modulePath.Path
	}
	manifestPath := filepath.Join(root, moduleconfig.DefaultConfigFile)
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return metadata, nil
		}
		return nil, fmt.Errorf("inspect selected module manifest %s: %w", manifestPath, statErr)
	}

	cfg, err := moduleconfig.Load(root)
	if err != nil {
		return nil, fmt.Errorf("load selected module manifest %s: %w", manifestPath, err)
	}
	if cfg.Organization != "" && cfg.ModuleName != "" && cfg.FullName() != moduleName {
		return nil, fmt.Errorf("selected module manifest identifies %q, want %q", cfg.FullName(), moduleName)
	}
	if cfg.Organization != "" && cfg.Organization != parsed.Organization {
		return nil, fmt.Errorf("selected module manifest organization %q does not match %q", cfg.Organization, parsed.Organization)
	}
	if cfg.ModuleName != "" && cfg.ModuleName != parsed.Module {
		return nil, fmt.Errorf("selected module manifest name %q does not match %q", cfg.ModuleName, parsed.Module)
	}

	return modulePackMetadataFromConfig(cfg, root, moduleName, modulePath.Version)
}

// modulePackMetadataFromConfig is shared by publish and lock-selected module
// packing so both paths emit the same module identity and publication fields.
func modulePackMetadataFromConfig(
	cfg *moduleconfig.ModuleConfig,
	baseDir string,
	moduleName string,
	fallbackVersion string,
) (attrs.Bag, error) {
	parsed, err := graph.ParseName(moduleName)
	if err != nil {
		return nil, fmt.Errorf("invalid module %q: %w", moduleName, err)
	}
	metadata := attrs.Bag{
		"name":      parsed.Module,
		"namespace": parsed.Organization + "." + parsed.Module,
		"version":   fallbackVersion,
	}
	if cfg.Version != "" {
		metadata["version"] = cfg.Version
	}
	if cfg.Description != "" {
		metadata["description"] = cfg.ResolveDescription(baseDir)
	}
	if cfg.License != "" {
		metadata["license"] = cfg.License
	}
	if cfg.Repository != "" {
		metadata["repository"] = cfg.Repository
	}
	if cfg.Homepage != "" {
		metadata["homepage"] = cfg.Homepage
	}
	if len(cfg.Keywords) > 0 {
		metadata["keywords"] = cfg.Keywords
	}
	if len(cfg.Authors) > 0 {
		metadata["authors"] = cfg.Authors
	}
	for key, value := range cfg.Metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := metadata[key]; exists {
			continue
		}
		metadata[key] = value
	}
	if err := addPublishedRuntimeMetadata(metadata, baseDir, cfg.Publish); err != nil {
		return nil, err
	}
	return metadata, nil
}

func validateModulePackIdentity(metadata attrs.Bag, moduleName string) error {
	parsed, err := graph.ParseName(moduleName)
	if err != nil {
		return fmt.Errorf("invalid module %q: %w", moduleName, err)
	}

	wantNamespace := parsed.Organization + "." + parsed.Module
	if rawName, present := metadata["name"]; present {
		name, ok := rawName.(string)
		if !ok || name == "" {
			return fmt.Errorf("selected module pack metadata has invalid name type %T", rawName)
		}
		if name != parsed.Module {
			return fmt.Errorf("selected module pack metadata name %q does not match %q", name, parsed.Module)
		}
	}
	if rawNamespace, present := metadata["namespace"]; present {
		namespace, ok := rawNamespace.(string)
		if !ok || namespace == "" {
			return fmt.Errorf("selected module pack metadata has invalid namespace type %T", rawNamespace)
		}
		if namespace != wantNamespace {
			return fmt.Errorf("selected module pack metadata namespace %q does not match %q", namespace, wantNamespace)
		}
	}
	if rawVersion, present := metadata["version"]; present {
		version, ok := rawVersion.(string)
		if !ok || version == "" {
			return fmt.Errorf("selected module pack metadata has invalid version type %T", rawVersion)
		}
	}
	return nil
}

func validateModulePackIdentityOverride(metadata, selected attrs.Bag) error {
	for _, key := range []string{"name", "namespace", "version"} {
		want, wantOK := selected[key]
		got, gotOK := metadata[key]
		if !wantOK || !gotOK || !reflect.DeepEqual(got, want) {
			return fmt.Errorf("module pack metadata key %q cannot override the selected module identity", key)
		}
	}
	return nil
}
