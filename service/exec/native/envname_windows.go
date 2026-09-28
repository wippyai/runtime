// SPDX-License-Identifier: MPL-2.0

//go:build windows

package native

import "strings"

func normalizeEnvironmentName(name string) string { return strings.ToUpper(name) }
