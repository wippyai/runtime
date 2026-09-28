// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package native

func normalizeEnvironmentName(name string) string { return name }
