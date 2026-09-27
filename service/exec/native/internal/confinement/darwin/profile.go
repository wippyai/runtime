// SPDX-License-Identifier: MPL-2.0

// Package darwin compiles the macOS Seatbelt policy used by native exec
// confinement. Launch and descriptor binding live in Darwin-only files; the
// compiler stays portable so its output can be unit-tested on every host.
package darwin

import (
	"strings"
)

type Profile struct {
	AllowFork bool
}

// CompileProfile emits only the policy this backend can actually deploy:
// unrestricted filesystem and networking with optional fork prevention.
func CompileProfile(policy Profile) string {
	var out strings.Builder
	out.WriteString("(version 1)\n(deny default)\n")
	out.WriteString("(allow signal (target self))\n")
	out.WriteString("(allow process-info-pidinfo (target self))\n")
	out.WriteString("(allow process-info-setcontrol (target self))\n")
	out.WriteString("(allow sysctl-read)\n")
	out.WriteString("(allow file-read-metadata (subpath \"/\"))\n")
	out.WriteString("(allow file-read-data file-write-data file-ioctl (vnode-type FIFO))\n")
	out.WriteString("(allow file-read-data (literal \"/dev/null\") (literal \"/dev/random\") (literal \"/dev/urandom\"))\n")
	if policy.AllowFork {
		out.WriteString("(allow process-fork)\n")
	}
	out.WriteString("(allow network*)\n")
	out.WriteString("(allow mach-lookup (global-name \"com.apple.SystemConfiguration.configd\") (global-name \"com.apple.system.opendirectoryd.libinfo\") (global-name \"com.apple.mDNSResponder\"))\n")
	out.WriteString("(allow file-read* file-write* file-map-executable process-exec)\n")
	return out.String()
}
