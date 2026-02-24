// SPDX-License-Identifier: MPL-2.0

package entries

import "strings"

// sourceKind describes an entry kind whose "source" data field holds file content
// that should be externalized to a separate file during wapp extraction.
//
// The loader reads these files back via file:// interpolation, so the round-trip is:
//
//	pack: source code string -> wapp entry data
//	extract: wapp entry data -> source file on disk + source: file://name.ext in _index.yaml
//	load: file:// interpolation reads the file back into the data field
//
// To add support for a new source-bearing entry kind, add it to sourceKinds below.
var sourceKinds = map[string]sourceKind{
	"function.lua": {ext: ".lua"},
	"library.lua":  {ext: ".lua"},
	"process.lua":  {ext: ".lua"},
	"workflow.lua": {ext: ".lua"},
	"template.jet": {ext: ".jet"},
	// todo: move to dynamic boot declaration
}

type sourceKind struct {
	ext string // file extension including the dot (e.g., ".lua")
}

// sourceExtForKind returns the file extension for externalizing the source field
// of the given entry kind. Returns empty string if the kind does not support
// source extraction.
//
// Lookup order:
//  1. Exact match in sourceKinds map
//  2. Suffix match: if the kind ends with a registered suffix (e.g., "custom.lua"
//     matches ".lua"), use that extension
func sourceExtForKind(kind string) string {
	if sk, ok := sourceKinds[kind]; ok {
		return sk.ext
	}

	// Suffix-based fallback: derive extension from kind suffix
	if idx := strings.LastIndex(kind, "."); idx >= 0 {
		suffix := kind[idx:]
		for _, sk := range sourceKinds {
			if sk.ext == suffix {
				return sk.ext
			}
		}
	}

	return ""
}
