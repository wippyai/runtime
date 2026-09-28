// SPDX-License-Identifier: MPL-2.0

package cmd

// machineLocalRuntimeSections are runtime config sections that describe the
// machine a deployment runs on. Publishing never writes them into pack
// metadata, and reading pack metadata rejects them.
var machineLocalRuntimeSections = map[string]struct{}{
	"boot":       {}, // derived by the runtime for the destination workspace
	"extensions": {}, // native extension paths belong to the build machine
	"workspace":  {}, // replacements and source roots belong to the workspace
}
