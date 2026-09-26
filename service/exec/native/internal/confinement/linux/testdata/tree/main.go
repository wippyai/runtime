// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func main() {
	marker := os.Getenv("HEARTBEAT")
	if marker == "" {
		os.Exit(50)
	}
	if len(os.Args) > 1 && os.Args[1] == "child" {
		deadline := time.Now().Add(2 * time.Second)
		for count := 1; time.Now().Before(deadline); count++ {
			if err := os.WriteFile(marker, []byte(strconv.Itoa(count)), 0600); err != nil {
				os.Exit(51)
			}
			time.Sleep(20 * time.Millisecond)
		}
		return
	}
	self := os.Args[0]
	if self == "" {
		os.Exit(52)
	}
	if err := os.WriteFile(marker+".root", []byte("root"), 0600); err != nil {
		fmt.Fprintln(os.Stderr, "write root marker:", err)
		os.Exit(55)
	}
	child := exec.Command(self, "child")
	child.Env = os.Environ()
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start descendant:", err)
		os.Exit(53)
	}
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(marker); err == nil {
			fmt.Println("child-ready")
			if len(os.Args) > 1 && os.Args[1] == "wait" {
				for {
					time.Sleep(time.Second)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(54)
}
