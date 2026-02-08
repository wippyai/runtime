// Package banner provides the wippy ASCII banner display functionality.
package banner

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/wippyai/runtime/api/version"
)

// GradientTheme defines a 3-step gradient color theme.
type GradientTheme struct {
	Logo1       int
	Logo2       int
	Logo3       int
	Title       int
	URL         int
	Version     int
	Company     int
	VersionBold bool
	CompanyBold bool
}

// Predefined gradients based on user preference model.
var allThemes = []GradientTheme{
	// Original favorites
	{220, 214, 208, 180, 214, 255, 208, true, true}, // Gold/Bronze
	{51, 45, 39, 245, 45, 255, 39, true, true},      // Cyan/Blue
	{135, 171, 207, 245, 177, 255, 207, true, true}, // Purple/Pink
	{154, 118, 82, 245, 120, 255, 82, true, true},   // Green/Forest

	// Sky Blue family (most preferred - 10x)
	{67, 73, 79, 180, 73, 255, 79, true, true},
	{69, 73, 77, 180, 73, 255, 77, true, true},
	{68, 72, 76, 180, 72, 255, 76, true, true},
	{67, 74, 81, 180, 74, 255, 81, true, true},

	// Green/Forest family (9x)
	{70, 76, 82, 180, 76, 255, 82, true, true},
	{74, 79, 85, 180, 79, 255, 85, true, true},
	{72, 76, 80, 180, 76, 255, 80, true, true},
	{70, 78, 87, 180, 78, 255, 87, true, true},

	// Teal/Aqua family (5x)
	{30, 35, 38, 180, 35, 255, 38, true, true},
	{32, 35, 38, 180, 35, 255, 38, true, true},
	{30, 37, 45, 180, 37, 255, 45, true, true},
	{31, 34, 38, 180, 34, 255, 38, true, true},

	// Cyan/Blue family (4x)
	{37, 44, 51, 180, 44, 255, 51, true, true},
	{38, 42, 47, 180, 42, 255, 47, true, true},
	{39, 46, 51, 180, 46, 255, 51, true, true},

	// Red/Orange family (4x)
	{196, 202, 206, 180, 202, 255, 206, true, true},
	{199, 203, 207, 180, 203, 255, 207, true, true},
	{198, 202, 206, 180, 202, 255, 206, true, true},
	{199, 204, 210, 180, 204, 255, 210, true, true},

	// Magenta/Pink family (4x)
	{163, 168, 174, 180, 168, 255, 174, true, true},
	{165, 170, 175, 180, 170, 255, 175, true, true},
	{165, 169, 174, 180, 169, 255, 174, true, true},
	{168, 179, 191, 180, 179, 255, 191, true, true},

	// Gold/Bronze variations (3x)
	{208, 217, 226, 180, 217, 255, 226, true, true},
	{210, 213, 217, 180, 213, 255, 217, true, true},
	{214, 223, 226, 180, 223, 255, 226, true, true},

	// Deep Blue family (2x)
	{25, 29, 33, 180, 29, 255, 33, true, true},
	{26, 29, 33, 180, 29, 255, 33, true, true},
	{25, 35, 45, 180, 35, 255, 45, true, true},

	// Green/Sage family (2x)
	{108, 114, 120, 180, 114, 255, 120, true, true},
	{108, 118, 120, 180, 118, 255, 120, true, true},

	// Purple/Violet family (2x)
	{127, 134, 141, 180, 134, 255, 141, true, true},
	{129, 132, 135, 180, 132, 255, 135, true, true},

	// Ruby family (deep red/burgundy)
	{197, 160, 124, 180, 160, 255, 124, true, true},
	{196, 161, 126, 180, 161, 255, 126, true, true},
	{197, 162, 127, 180, 162, 255, 127, true, true},
}

// PrintWithTheme displays the banner using the specified gradient theme.
func PrintWithTheme(silent bool, theme GradientTheme) {
	if silent {
		return
	}

	buildDate := version.Date
	if idx := strings.Index(buildDate, "T"); idx > 0 {
		buildDate = buildDate[:idx]
	}

	logo1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.Logo1)))
	logo2 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.Logo2)))
	logo3 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.Logo3)))
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.Title)))
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.URL)))
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.Version)))
	if theme.VersionBold {
		versionStyle = versionStyle.Bold(true)
	}
	companyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", theme.Company)))
	if theme.CompanyBold {
		companyStyle = companyStyle.Bold(true)
	}

	fmt.Println()
	fmt.Printf("  %s  %s %s\n",
		logo1.Render("╦ ╦╦╔═╗╔═╗╦ ╦"),
		titleStyle.Render("Adaptive Application Runtime"),
		urlStyle.Render("https://wippy.ai"))
	fmt.Printf("  %s  %s %s\n",
		logo2.Render("║║║║╠═╝╠═╝╚╦╝"),
		versionStyle.Render(version.Version),
		titleStyle.Render(buildDate))
	fmt.Printf("  %s  %s %s\n",
		logo3.Render("╚╩╝╩╩  ╩   ╩ "),
		titleStyle.Render("by"),
		companyStyle.Render("Spiral Scout"))
	fmt.Println()
}

// Print displays the wippy ASCII banner with a random gradient from the curated collection.
// If silent is true, no output is produced.
func Print(silent bool) {
	theme := allThemes[rand.IntN(len(allThemes))] //nolint:gosec // decorative randomness
	PrintWithTheme(silent, theme)
}
