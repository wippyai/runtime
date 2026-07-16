// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
)

const (
	metaModuleKey        = "module"
	metaModuleVersionKey = "module_version"
	metaModuleDigestKey  = "module_digest"
)

type moduleOwner struct {
	name    string
	version string
	digest  string
}

func moduleOwnersByNamespace(modules []ResolvedModule) map[string]moduleOwner {
	owners := make(map[string]moduleOwner, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		namespace := mod.Org + "." + mod.Name
		owners[namespace] = moduleOwner{
			name:    mod.Org + "/" + mod.Name,
			version: mod.Version,
			digest:  mod.Digest,
		}
	}
	return owners
}

func moduleOwnersByEntryID(entries regapi.State) map[string]moduleOwner {
	owners := make(map[string]moduleOwner, len(entries))
	for _, entry := range entries {
		module := entryModule(entry)
		if module == "" {
			continue
		}
		owners[idKey(entry.ID)] = moduleOwner{
			name:    module,
			version: moduleVersion(entry),
			digest:  moduleDigest(entry),
		}
	}
	return owners
}

func snapshotModuleVersions(snapshot regapi.State) map[string]string {
	versions := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, entry := range snapshot {
		module := entryModule(entry)
		if module == "" || entry.Meta == nil {
			continue
		}
		raw, ok := entry.Meta[metaModuleVersionKey]
		if !ok {
			continue
		}
		version, ok := raw.(string)
		if !ok || version == "" {
			continue
		}
		if _, bad := ambiguous[module]; bad {
			continue
		}
		if existing, seen := versions[module]; seen && existing != version {
			delete(versions, module)
			ambiguous[module] = struct{}{}
			continue
		}
		versions[module] = version
	}
	return versions
}

func snapshotModuleDigests(snapshot regapi.State) map[string]string {
	digests := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, entry := range snapshot {
		module := entryModule(entry)
		digest := moduleDigest(entry)
		if module == "" || digest == "" {
			continue
		}
		if _, bad := ambiguous[module]; bad {
			continue
		}
		if existing, seen := digests[module]; seen && !strings.EqualFold(existing, digest) {
			delete(digests, module)
			ambiguous[module] = struct{}{}
			continue
		}
		digests[module] = digest
	}
	return digests
}

func entryModule(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func moduleVersion(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleVersionKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func moduleDigest(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleDigestKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func markModuleMeta(entry regapi.Entry, moduleName, moduleVersion string) regapi.Entry {
	meta := entry.Meta
	if meta == nil {
		meta = attrs.NewBag()
	} else {
		meta = attrs.NewBagFrom(meta)
	}
	existingModule := strings.TrimSpace(meta.GetString(metaModuleKey, ""))
	if existingModule == "" {
		meta.Set(metaModuleKey, moduleName)
		if moduleVersion != "" {
			meta.Set(metaModuleVersionKey, moduleVersion)
		}
	} else if existingModule == moduleName && moduleVersion != "" && meta.GetString(metaModuleVersionKey, "") == "" {
		meta.Set(metaModuleVersionKey, moduleVersion)
	}
	entry.Meta = meta
	return entry
}

func markModuleIdentity(entry regapi.Entry, moduleName, moduleVersion, digest string) regapi.Entry {
	entry = markModuleMeta(entry, moduleName, moduleVersion)
	if entryModule(entry) != moduleName || digest == "" {
		return entry
	}
	meta := attrs.NewBagFrom(entry.Meta)
	meta.Set(metaModuleDigestKey, digest)
	entry.Meta = meta
	return entry
}

func markModuleIdentityForGraph(
	entry regapi.Entry,
	moduleName string,
	moduleVersion string,
	digest string,
	namespaceOwners map[string]moduleOwner,
	entryOwners map[string]moduleOwner,
) regapi.Entry {
	if entryModule(entry) != "" {
		return markModuleIdentity(entry, moduleName, moduleVersion, digest)
	}
	if owner, ok := entryOwners[idKey(entry.ID)]; ok && owner.name != "" {
		if owner.name == moduleName {
			return markModuleIdentity(entry, moduleName, moduleVersion, digest)
		}
		return markModuleIdentity(entry, owner.name, owner.version, owner.digest)
	}
	if owner, ok := namespaceOwners[entry.ID.NS]; ok && owner.name != "" {
		return markModuleIdentity(entry, owner.name, owner.version, owner.digest)
	}
	return markModuleIdentity(entry, moduleName, moduleVersion, digest)
}

func markModuleMetaForGraph(
	entry regapi.Entry,
	moduleName string,
	moduleVersion string,
	namespaceOwners map[string]moduleOwner,
	entryOwners map[string]moduleOwner,
) regapi.Entry {
	if entryModule(entry) != "" {
		return markModuleMeta(entry, moduleName, moduleVersion)
	}
	if owner, ok := entryOwners[idKey(entry.ID)]; ok && owner.name != "" {
		if owner.name == moduleName {
			return markModuleMeta(entry, moduleName, moduleVersion)
		}
		return markModuleMeta(entry, owner.name, owner.version)
	}
	if owner, ok := namespaceOwners[entry.ID.NS]; ok && owner.name != "" {
		return markModuleMeta(entry, owner.name, owner.version)
	}
	return markModuleMeta(entry, moduleName, moduleVersion)
}
