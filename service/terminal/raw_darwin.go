//go:build darwin

package terminal

import "golang.org/x/sys/unix"

func enableOutputProcessing(fd int) {
	termios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return
	}
	termios.Oflag |= unix.OPOST
	_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, termios)
}
