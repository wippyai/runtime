// Package banner provides the wippy ASCII banner display functionality.
package banner

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/wippyai/runtime/cmd/wippy/version"
)

// Print displays the wippy ASCII banner with version information.
// If silent is true, no output is produced.
func Print(silent bool) {
	if silent {
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
	fmt.Printf("  %s  %s %s\n",
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
