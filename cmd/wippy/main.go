package main

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/ponyruntime/pony/cmd/wippy/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		if cmd.IsConsoleMode() {
			// Pretty error output for console mode
			red := color.New(color.FgRed, color.Bold)
			red.Fprint(os.Stderr, "✗ Error: ")
			fmt.Fprintln(os.Stderr, err)
		} else {
			// Plain error output
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
