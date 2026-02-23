// SPDX-License-Identifier: MPL-2.0

//go:build linux

package terminal

import "golang.org/x/sys/unix"

func enableOutputProcessing(fd int) {
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return
	}
	termios.Oflag |= unix.OPOST
	_ = unix.IoctlSetTermios(fd, unix.TCSETS, termios)
}
