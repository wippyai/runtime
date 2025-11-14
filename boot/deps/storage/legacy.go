package storage

import (
	"strings"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// stripLegacyPrefix removes the legacy "module-*/" prefix from file paths.
//
// LEGACY BEHAVIOR: The download service historically sent files with paths like:
//
//	"module-llm-v0.0.11/llm.lua"
//	"module-actor-v1.2.3/actor.lua"
//
// This function strips the first path component if it starts with "module-".
//
// Example:
//
//	"module-llm-v0.0.11/llm.lua" -> "llm.lua"
//	"module-actor/subdir/file.txt" -> "subdir/file.txt"
//	"regular/path.lua" -> "regular/path.lua" (unchanged)
//
// TODO: Remove this function when the download service stops sending the prefix.
func stripLegacyPrefix(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 1 && strings.HasPrefix(parts[0], "module-") {
		return strings.Join(parts[1:], "/")
	}
	return path
}

// detectLegacyPrefix checks if the files use the legacy "module-*/" prefix format.
//
// Returns true if the first file's path starts with a "module-" directory.
// This is used to determine whether stripLegacyPrefix should be applied.
//
// LEGACY BEHAVIOR: Should return false once the download service is updated.
func detectLegacyPrefix(files []*modulev1.File) bool {
	if len(files) == 0 {
		return false
	}

	firstPath := files[0].GetPath()
	parts := strings.Split(firstPath, "/")

	return len(parts) > 1 && strings.HasPrefix(parts[0], "module-")
}
