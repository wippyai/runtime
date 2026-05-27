// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
	"github.com/wippyai/runtime/boot/deps/graph"
	boothub "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
)

// ArtifactClient is the small download surface needed for read-only artifact
// inspection. The boot Hub client satisfies it; tests can provide a fake.
type ArtifactClient interface {
	GetDownloadURL(context.Context, *boothub.DownloadParams) (*boothub.DownloadInfo, error)
	DownloadToFile(context.Context, string, string) error
}

type artifactInspection struct {
	Version      string
	Digest       string
	Path         string
	Requirements []*versionv1.Requirement
	EntryKinds   []string
	SizeBytes    uint64
	EntryCount   int
	Protected    bool
}

func inspectVersionArtifact(ctx context.Context, client ArtifactClient, params *boothub.DownloadParams, vendorDir string) (*artifactInspection, error) {
	if client == nil {
		return nil, fmt.Errorf("artifact client required")
	}
	info, err := client.GetDownloadURL(ctx, params)
	if err != nil {
		return nil, err
	}
	if info == nil || strings.TrimSpace(info.URL) == "" {
		return nil, fmt.Errorf("artifact download URL unavailable")
	}

	path, pathErr := artifactCachePath(params, info, vendorDir)
	if pathErr != nil {
		return nil, pathErr
	}
	displayVersion := firstNonEmpty(info.Version, params.Version, params.VersionID, params.Label)
	if _, statErr := os.Stat(path); statErr == nil {
		if err := boothub.VerifyDownloadedArtifact(path, info.Digest, info.Size); err == nil {
			inspection, inspectErr := inspectCachedArtifact(path, info, displayVersion)
			if inspectErr == nil {
				return inspection, nil
			}
		}
		_ = os.Remove(path)
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create artifact cache directory: %w", err)
	}
	if err := client.DownloadToFile(ctx, info.URL, path); err != nil {
		return nil, err
	}
	if err := boothub.VerifyDownloadedArtifact(path, info.Digest, info.Size); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("verify artifact: %w", err)
	}
	return inspectCachedArtifact(path, info, displayVersion)
}

func inspectCachedArtifact(path string, info *boothub.DownloadInfo, version string) (*artifactInspection, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	entries, err := reader.GetEntries()
	if err != nil {
		return nil, fmt.Errorf("read artifact entries: %w", err)
	}

	kinds := make(map[string]struct{})
	requirements := make([]*versionv1.Requirement, 0)
	for _, entry := range entries {
		if entry.Kind != "" {
			kinds[entry.Kind] = struct{}{}
		}
		if entry.Kind != "ns.requirement" {
			continue
		}
		requirements = append(requirements, requirementFromWappEntry(entry))
	}

	entryKinds := make([]string, 0, len(kinds))
	for kind := range kinds {
		entryKinds = append(entryKinds, kind)
	}
	sort.Strings(entryKinds)

	return &artifactInspection{
		Version:      version,
		Digest:       info.Digest,
		SizeBytes:    info.Size,
		Protected:    info.Protected,
		Path:         path,
		EntryCount:   len(entries),
		EntryKinds:   entryKinds,
		Requirements: requirements,
	}, nil
}

func artifactCachePath(params *boothub.DownloadParams, info *boothub.DownloadInfo, vendorDir string) (string, error) {
	vendorDir = strings.TrimSpace(vendorDir)
	if vendorDir == "" {
		vendorDir = filepath.Join(".wippy", "vendor")
	}
	if params == nil {
		return "", fmt.Errorf("download parameters required")
	}
	version := firstNonEmpty(info.Version, params.Version)
	if version == "" {
		return "", fmt.Errorf("resolved artifact version required for cache path")
	}
	if params.Org == "" || params.Module == "" {
		if params.ModuleID == "" {
			return "", fmt.Errorf("module org/name required for cache path")
		}
		return filepath.Join(
			vendorDir,
			"_ids",
			sanitizeArtifactCachePart(params.ModuleID)+"-"+sanitizeArtifactCachePart(version)+".wapp",
		), nil
	}
	name, err := graph.ParseName(params.Org + "/" + params.Module)
	if err != nil {
		return "", err
	}
	return filepath.Join(vendorDir, lock.WappPath(name, version)), nil
}

func sanitizeArtifactCachePart(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "artifact"
	}
	return out
}

func downloadParamsFromRefs(moduleRef *modulev1.ModuleRef, versionRef *versionv1.VersionRef) (*boothub.DownloadParams, error) {
	params := &boothub.DownloadParams{}
	if moduleRef == nil {
		return nil, fmt.Errorf("module reference required")
	}
	switch ref := moduleRef.Value.(type) {
	case *modulev1.ModuleRef_Name:
		if ref.Name == nil || ref.Name.Org == "" || ref.Name.Name == "" {
			return nil, fmt.Errorf("module name must include org and name")
		}
		params.Org = ref.Name.Org
		params.Module = ref.Name.Name
	case *modulev1.ModuleRef_Id:
		if ref.Id == "" {
			return nil, fmt.Errorf("module id required")
		}
		params.ModuleID = ref.Id
	default:
		return nil, fmt.Errorf("module reference required")
	}

	if versionRef == nil {
		return nil, fmt.Errorf("version reference required")
	}
	switch ref := versionRef.Value.(type) {
	case *versionv1.VersionRef_Id:
		if ref.Id == "" {
			return nil, fmt.Errorf("version id required")
		}
		params.VersionID = ref.Id
	case *versionv1.VersionRef_Version:
		if ref.Version == "" {
			return nil, fmt.Errorf("version required")
		}
		params.Version = ref.Version
	case *versionv1.VersionRef_Label:
		if ref.Label == "" {
			return nil, fmt.Errorf("version label required")
		}
		params.Label = ref.Label
	default:
		return nil, fmt.Errorf("version reference required")
	}
	return params, nil
}

func requirementFromWappEntry(entry wapp.Entry) *versionv1.Requirement {
	data := mapFromAny(entry.Data)
	name := entry.ID.Name
	if name == "" {
		name = entry.ID.String()
	}
	req := &versionv1.Requirement{
		Name:        name,
		Description: stringFromMetadata(entry.Meta, "description"),
		Default:     stringFromAny(data["default"]),
		Targets:     requirementTargetsFromAny(data["targets"]),
	}
	if req.Description == "" {
		req.Description = stringFromAny(data["description"])
	}
	return req
}

func requirementTargetsFromAny(value any) []*versionv1.RequirementTarget {
	items := sliceFromAny(value)
	out := make([]*versionv1.RequirementTarget, 0, len(items))
	for _, item := range items {
		row := mapFromAny(item)
		entry := stringFromAny(row["entry"])
		path := stringFromAny(row["path"])
		if entry == "" && path == "" {
			continue
		}
		out = append(out, &versionv1.RequirementTarget{Entry: entry, Path: path})
	}
	return out
}

func mapFromAny(value any) map[string]any {
	switch m := value.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, v := range m {
			if key, ok := k.(string); ok {
				out[key] = v
			}
		}
		return out
	default:
		return map[string]any{}
	}
}

func sliceFromAny(value any) []any {
	switch items := value.(type) {
	case []any:
		return items
	case []map[string]any:
		out := make([]any, len(items))
		for i := range items {
			out[i] = items[i]
		}
		return out
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func stringFromMetadata(meta wapp.Metadata, key string) string {
	if meta == nil {
		return ""
	}
	return stringFromAny(meta[key])
}
