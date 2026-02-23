// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for modules in the hub",
	Long: `Search for modules in the wippy hub.

Examples:
  wippy search http              # Search for http modules
  wippy search --json http       # Output as JSON
  wippy search --limit 10 http   # Limit results`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)

	searchCmd.Flags().Bool("json", false, "output as JSON")
	searchCmd.Flags().Int32("limit", 20, "max results")
	searchCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")
	limit, _ := cmd.Flags().GetInt32("limit")
	registryURL, _ := cmd.Flags().GetString("registry")

	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	client, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
	})
	if err != nil {
		return NewSearchClientError(registryURL, err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	result, err := client.SearchModules(ctx, &hub.SearchParams{
		Query:    query,
		PageSize: limit,
	})
	if err != nil {
		return NewSearchError(query, registryURL, err)
	}

	if jsonOutput {
		return printSearchJSON(result)
	}

	return printSearchTable(result)
}

func printSearchJSON(result *hub.SearchResult) error {
	type moduleJSON struct {
		Name        string `json:"name"`
		Org         string `json:"org"`
		Version     string `json:"version"`
		Description string `json:"description,omitempty"`
		Downloads   uint64 `json:"downloads"`
	}

	modules := make([]moduleJSON, 0, len(result.Modules))
	for _, m := range result.Modules {
		modules = append(modules, moduleJSON{
			Name:        m.Name,
			Org:         m.Org,
			Version:     m.LatestVersion,
			Description: m.Description,
			Downloads:   m.Downloads,
		})
	}

	data, err := json.MarshalIndent(modules, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}

func printSearchTable(result *hub.SearchResult) error {
	if len(result.Modules) == 0 {
		fmt.Println("No modules found.")
		return nil
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println()
	fmt.Printf("%s  %s  %s\n",
		titleStyle.Render(padRight("NAME", 30)),
		titleStyle.Render(padRight("VERSION", 12)),
		titleStyle.Render("DESCRIPTION"))
	fmt.Println(dimStyle.Render(strings.Repeat("-", 80)))

	for _, m := range result.Modules {
		name := fmt.Sprintf("%s/%s", m.Org, m.Name)
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		desc := m.Description
		if len(desc) > 35 {
			desc = desc[:32] + "..."
		}

		fmt.Printf("%s  %s  %s\n",
			nameStyle.Render(padRight(name, 30)),
			versionStyle.Render(padRight(m.LatestVersion, 12)),
			desc)
	}

	fmt.Println()
	fmt.Printf("%s %d of %d results\n", dimStyle.Render("Showing"), len(result.Modules), result.TotalCount)
	fmt.Println()

	return nil
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func NewSearchClientError(registryURL string, cause error) error {
	return fmt.Errorf("failed to create hub client for %s: %w", registryURL, cause)
}

func NewSearchError(query, registryURL string, cause error) error {
	return fmt.Errorf("search failed for %q on %s: %w", query, registryURL, cause)
}
