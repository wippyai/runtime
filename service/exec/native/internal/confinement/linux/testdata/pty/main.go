// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func printSize() {
	size, err := unix.IoctlGetWinsize(0, unix.TIOCGWINSZ)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(60)
	}
	fmt.Printf("%d %d\n", size.Row, size.Col)
}

func main() {
	printSize()
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(61)
	}
	printSize()
}
