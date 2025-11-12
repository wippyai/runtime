package main

import (
	"os"

	"github.com/ponyruntime/pony/cmd/wippy/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
