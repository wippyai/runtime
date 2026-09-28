// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(90)
	}
	switch os.Args[1] {
	case "environment":
		fmt.Printf("%s\n%s\n%s\n", os.Getenv("WIPPY_PINNED"), os.Getenv("HOME"), os.Getenv("TMPDIR"))
	case "cwd":
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(97)
		}
		fmt.Println(cwd)
	case "filesystem":
		if len(os.Args) != 5 {
			os.Exit(91)
		}
		payload, err := os.ReadFile(os.Args[2])
		if err != nil {
			os.Exit(92)
		}
		if _, err := os.ReadFile(os.Args[3]); err == nil {
			os.Exit(93)
		}
		if err := os.WriteFile(os.Args[4], []byte("written"), 0o600); err != nil {
			os.Exit(94)
		}
		fmt.Printf("%s:%s", os.Getenv("WIPPY_PINNED"), payload)
	case "spawn-denied":
		child := exec.Command(os.Args[0], "sleep")
		if err := child.Start(); err == nil {
			_ = child.Process.Kill()
			os.Exit(95)
		}
		fmt.Println("SPAWN_DENIED")
		time.Sleep(time.Hour)
	case "sleep":
		time.Sleep(time.Hour)
	default:
		os.Exit(96)
	}
}
