package cmd

import (
	"fmt"

	"github.com/fatih/color"
)

func printBanner() {
	if silentLogs {
		return
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	fmt.Println()
	fmt.Println(cyan("  ╦ ╦╦╔═╗╔═╗╦ ╦"))
	fmt.Println(cyan("  ║║║║╠═╝╠═╝╚╦╝"))
	fmt.Println(cyan("  ╚╩╝╩╩  ╩   ╩ "))
	fmt.Println()
	fmt.Println(gray("  Runtime for AI agents and adaptive systems"))
	fmt.Println(gray("  ") + green("https://wippy.ai"))
	fmt.Println()
}
