// SPDX-License-Identifier: MPL-2.0

// Package darwin compiles the macOS Seatbelt policy used by native exec
// confinement. Launch and descriptor binding live in Darwin-only files; the
// compiler stays portable so its output can be unit-tested on every host.
package darwin

import (
	"errors"
	"fmt"
	"strings"
)

type Grant struct {
	Path              string
	Read, Write, Exec bool
}

type Profile struct {
	Grants                 []Grant
	FilesystemUnrestricted bool
	NetworkUnrestricted    bool
	AllowFork              bool
}

func CompileProfile(policy Profile) (string, error) {
	var out strings.Builder
	out.WriteString("(version 1)\n(deny default)\n")
	out.WriteString("(allow signal (target self))\n")
	out.WriteString("(allow process-info-pidinfo (target self))\n")
	out.WriteString("(allow sysctl-read)\n")
	out.WriteString("(allow file-read-metadata (subpath \"/\"))\n")
	out.WriteString("(allow file-read-data (literal \"/dev/null\") (literal \"/dev/random\") (literal \"/dev/urandom\"))\n")
	if policy.AllowFork {
		out.WriteString("(allow process-fork)\n")
	}
	if policy.NetworkUnrestricted {
		out.WriteString("(allow network*)\n")
		out.WriteString("(allow mach-lookup (global-name \"com.apple.SystemConfiguration.configd\") (global-name \"com.apple.system.opendirectoryd.libinfo\") (global-name \"com.apple.mDNSResponder\"))\n")
	}
	if policy.FilesystemUnrestricted {
		out.WriteString("(allow file-read* file-write* file-map-executable process-exec)\n")
		return out.String(), nil
	}
	for _, grant := range policy.Grants {
		path, err := quote(grant.Path)
		if err != nil {
			return "", err
		}
		filter := "(subpath " + path + ")"
		if grant.Read || grant.Write {
			out.WriteString("(allow file-read* " + filter + ")\n")
		}
		if grant.Write {
			out.WriteString("(allow file-write-data file-write-create file-write-unlink " + filter + ")\n")
		}
		if grant.Exec {
			out.WriteString("(allow file-map-executable process-exec " + filter + ")\n")
		}
	}
	return out.String(), nil
}

func quote(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("invalid Seatbelt path")
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return fmt.Sprintf("\"%s\"", value), nil
}
