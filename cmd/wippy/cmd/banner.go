package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/ponyruntime/pony/cmd/wippy/version"
)

func printBanner() {
	if silentLogs {
		return
	}

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	white := color.New(color.FgWhite).SprintFunc()

	buildDate := version.Date
	if len(buildDate) > 10 {
		buildDate = buildDate[:10]
	}

	fmt.Println()
	fmt.Printf("  %s  %s  %s\n",
		cyan("╦ ╦╦╔═╗╔═╗╦ ╦"),
		gray("Adaptive Application Runtime"),
		green("https://wippy.ai"))
	fmt.Printf("  %s  %s %s\n",
		cyan("║║║║╠═╝╠═╝╚╦╝"),
		white(version.Version),
		gray(buildDate))
	fmt.Printf("  %s  %s %s\n",
		cyan("╚╩╝╩╩  ╩   ╩ "),
		gray("by"),
		cyan("Spiral Scout"))
	fmt.Println()
}
