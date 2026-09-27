// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	for _, name := range []string{"HOME", "TMPDIR"} {
		path := os.Getenv(name)
		if path == "" {
			os.Exit(41)
		}
		if err := os.WriteFile(filepath.Join(path, "probe"), []byte(name), 0600); err != nil {
			os.Exit(42)
		}
		if _, err := os.ReadFile(filepath.Join(path, "probe")); err != nil {
			os.Exit(43)
		}
		if err := os.Chmod(filepath.Join(path, "probe"), 0777); !errors.Is(err, syscall.EPERM) {
			os.Exit(45)
		}
		file, err := os.Open(filepath.Join(path, "probe"))
		if err != nil {
			os.Exit(46)
		}
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), 0x40086602, 0) // FS_IOC_SETFLAGS
		_ = file.Close()
		if errno != syscall.EPERM {
			os.Exit(47)
		}
	}
	if _, err := os.ReadFile("/etc/passwd"); err == nil {
		os.Exit(44)
	}
	fmt.Println("private-ok")
}
