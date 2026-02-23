// SPDX-License-Identifier: MPL-2.0

//go:build windows

package terminal

// Windows console does not use POSIX termios; output processing
// is handled by the console subsystem.
func enableOutputProcessing(fd int) {}
