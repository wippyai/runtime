// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	home := os.Getenv("HOME")
	if home == "" || home == "/.wippy-confine-private/home" {
		os.Exit(80)
	}
	if err := os.WriteFile(filepath.Join(home, "probe"), []byte("private"), 0600); err != nil {
		os.Exit(81)
	}
	if _, err := os.ReadFile("/etc/passwd"); err != nil {
		os.Exit(82)
	}
	fmt.Println(home)
	_, _ = io.Copy(io.Discard, os.Stdin)
}
