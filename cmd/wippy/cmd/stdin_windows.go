// SPDX-License-Identifier: MPL-2.0

//go:build windows

package cmd

import "syscall"

func stdinFd() int {
	return int(syscall.Stdin)
}
