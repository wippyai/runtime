package cmd

import (
	stdjson "encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/entries"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/registry/finder"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Query and inspect registry entries",
	Long: `Query and inspect registry entries from the lock file.

Use subcommands to list entries with filters or show specific entry content.

Examples:
  wippy registry list                          # List all entries
  wippy registry list --kind "function.lua.*"  # List Lua functions
  wippy registry list --ns "app"               # List entries in app namespace
  wippy registry list --meta "type=api"        # Filter by metadata
  wippy registry show app:hello                # Show entry details
  wippy registry show app:hello --field source # Show only source field`,
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registry entries",
	Long: `List registry entries with optional filters.

Filters use the Finder query syntax:
  --kind: Match entry kind (supports glob: "function.lua.*")
  --ns: Match namespace (supports glob: "app.*")
  --name: Match entry name (supports glob)
  --meta: Match metadata field (format: "field=value" or "field~regex")

Metadata operators:
  field=value   Exact match
  field~regex   Regex match
  field*substr  Contains substring
  field^prefix  Starts with prefix
  field$suffix  Ends with suffix

Examples:
  wippy registry list --kind "process.lua.*"
  wippy registry list --meta "type=api" --meta "enabled=true"
  wippy registry list --ns "app.*" --json`,
	RunE: runRegistryList,
}

var registryShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show registry entry details",
	Long: `Show details of a specific registry entry.

The entry ID can be specified as:
  - Full ID: "namespace:name"
  - Just name (if unambiguous): "name"

Use --field to show only a specific field from the entry data.

Examples:
  wippy registry show app:hello
  wippy registry show app:hello --field source
  wippy registry show app:hello --json`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryShow,
}

func init() {
	rootCmd.AddCommand(registryCmd)
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryShowCmd)

	// List flags
	registryListCmd.Flags().StringP("kind", "k", "", "filter by kind (glob pattern)")
	registryListCmd.Flags().StringP("ns", "n", "", "filter by namespace (glob pattern)")
	registryListCmd.Flags().String("name", "", "filter by name (glob pattern)")
	registryListCmd.Flags().StringSlice("meta", nil, "filter by metadata (field=value)")
	registryListCmd.Flags().Bool("json", false, "output in JSON format")
	registryListCmd.Flags().Bool("yaml", false, "output in YAML format")
	registryListCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")

	// Show flags
	registryShowCmd.Flags().StringP("field", "f", "", "show only specific field from data")
	registryShowCmd.Flags().Bool("json", false, "output in JSON format")
	registryShowCmd.Flags().Bool("yaml", false, "output in YAML format")
	registryShowCmd.Flags().Bool("raw", false, "output raw field value without formatting")
	registryShowCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
}

func runRegistryList(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	kindFilter, _ := cmd.Flags().GetString("kind")
	nsFilter, _ := cmd.Flags().GetString("ns")
	nameFilter, _ := cmd.Flags().GetString("name")
	metaFilters, _ := cmd.Flags().GetStringSlice("meta")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	yamlOutput, _ := cmd.Flags().GetBool("yaml")
	lockFile, _ := cmd.Flags().GetString("lock-file")

	allEntries, err := loadRegistryEntries(cmd, lockFile)
	if err != nil {
		return err
	}

	// Build finder query
	query := make(attrs.Bag)
	if kindFilter != "" {
		query[".kind"] = kindFilter
	}
	if nsFilter != "" {
		query[".ns"] = nsFilter
	}
	if nameFilter != "" {
		query[".name"] = nameFilter
	}

	// Parse metadata filters
	for _, mf := range metaFilters {
		key, value, op := parseMetaFilter(mf)
		if key != "" {
			query[op+"meta."+key] = value
		}
	}

	// Use finder if we have filters
	var results []regapi.Entry
	if len(query) > 0 {
		// Create a simple entry reader
		reader := &sliceEntryReader{entries: allEntries}
		f := finder.NewFinder(reader, zap.NewNop())
		results, err = f.Find(query)
		if err != nil {
			return fmt.Errorf("finder error: %w", err)
		}
	} else {
		results = allEntries
	}

	// Sort by ID
	sort.Slice(results, func(i, j int) bool {
		return results[i].ID.String() < results[j].ID.String()
	})

	if jsonOutput {
		return outputEntriesJSON(results)
	}
	if yamlOutput {
		return outputEntriesYAML(results)
	}

	// Table output
	outputEntriesTable(results)
	return nil
}

func runRegistryShow(cmd *cobra.Command, args []string) error {
	silentLogs = true

	entryID := args[0]
	fieldName, _ := cmd.Flags().GetString("field")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	yamlOutput, _ := cmd.Flags().GetBool("yaml")
	rawOutput, _ := cmd.Flags().GetBool("raw")
	lockFile, _ := cmd.Flags().GetString("lock-file")

	allEntries, err := loadRegistryEntries(cmd, lockFile)
	if err != nil {
		return err
	}

	// Find the entry
	var entry *regapi.Entry
	for i := range allEntries {
		e := &allEntries[i]
		if e.ID.String() == entryID {
			entry = e
			break
		}
		// Try matching just the name
		if e.ID.Name == entryID {
			if entry != nil {
				return fmt.Errorf("ambiguous entry name %q, use full ID (namespace:name)", entryID)
			}
			entry = e
		}
	}

	if entry == nil {
		return fmt.Errorf("entry not found: %s", entryID)
	}

	// If field specified, extract just that field
	if fieldName != "" {
		return showEntryField(entry, fieldName, jsonOutput, yamlOutput, rawOutput)
	}

	// Show full entry
	if jsonOutput {
		return outputEntryJSON(entry)
	}
	if yamlOutput || rawOutput {
		return outputEntryYAML(entry)
	}

	outputEntryTable(entry)
	return nil
}

func loadRegistryEntries(cmd *cobra.Command, lockFile string) ([]regapi.Entry, error) {
	logger, err := clilogger.CreateLogger(clilogger.Config{
		Silent:       true,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return nil, NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	lockPath, err := lock.Find(".", lockFile)
	if err != nil {
		return nil, NewLockFileNotFoundError(err)
	}

	app, err := appinit.Init(cmd.Context(), false, false, false, true, appStartTime)
	if err != nil {
		return nil, NewInitAppError(err)
	}

	if err := entries.EnsureModulesInstalled(app.Ctx, lockPath, logger); err != nil {
		return nil, NewEnsureModulesInstalledError(err)
	}

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return nil, NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return nil, NewInvalidLockFileError(err)
	}

	paths := lockObj.GetLoadPaths()

	var allEntries []regapi.Entry
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		dirFS := os.DirFS(path)
		pathEntries, err := app.Loader.LoadFS(app.Ctx, dirFS)
		if err != nil {
			return nil, NewLoadEntriesError(path, err)
		}
		allEntries = append(allEntries, pathEntries...)
	}

	return allEntries, nil
}

func parseMetaFilter(filter string) (key, value, operator string) {
	// Check for operators: ~, *, ^, $
	for _, op := range []string{"~", "*", "^", "$"} {
		if idx := strings.Index(filter, op); idx > 0 {
			return filter[:idx], filter[idx+1:], op
		}
	}
	// Default: exact match with =
	if idx := strings.Index(filter, "="); idx > 0 {
		return filter[:idx], filter[idx+1:], ""
	}
	return "", "", ""
}

// sliceEntryReader implements registry.EntryReader for a slice of entries
type sliceEntryReader struct {
	entries []regapi.Entry
}

func (r *sliceEntryReader) GetAllEntries() ([]regapi.Entry, error) {
	return r.entries, nil
}

func (r *sliceEntryReader) GetEntry(id regapi.ID) (regapi.Entry, error) {
	for _, e := range r.entries {
		if e.ID == id {
			return e, nil
		}
	}
	return regapi.Entry{}, fmt.Errorf("entry not found: %s", id.String())
}

func outputEntriesJSON(entries []regapi.Entry) error {
	type entryInfo struct {
		Meta attrs.Bag `json:"meta,omitempty"`
		ID   string    `json:"id"`
		Kind string    `json:"kind"`
	}

	infos := make([]entryInfo, len(entries))
	for i, e := range entries {
		infos[i] = entryInfo{
			ID:   e.ID.String(),
			Kind: e.Kind,
			Meta: e.Meta,
		}
	}

	data, err := stdjson.MarshalIndent(infos, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputEntriesYAML(entries []regapi.Entry) error {
	type entryInfo struct {
		Meta attrs.Bag `yaml:"meta,omitempty"`
		ID   string    `yaml:"id"`
		Kind string    `yaml:"kind"`
	}

	infos := make([]entryInfo, len(entries))
	for i, e := range entries {
		infos[i] = entryInfo{
			ID:   e.ID.String(),
			Kind: e.Kind,
			Meta: e.Meta,
		}
	}

	data, err := yaml.Marshal(infos)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func outputEntriesTable(entries []regapi.Entry) {
	if len(entries) == 0 {
		if console {
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
			fmt.Println(style.Render("No entries found"))
		} else {
			fmt.Println("No entries found")
		}
		return
	}

	// Find max widths
	maxID := 2
	maxKind := 4
	for _, e := range entries {
		if len(e.ID.String()) > maxID {
			maxID = len(e.ID.String())
		}
		if len(e.Kind) > maxKind {
			maxKind = len(e.Kind)
		}
	}

	// Cap widths
	if maxID > 50 {
		maxID = 50
	}
	if maxKind > 30 {
		maxKind = 30
	}

	// Styles for console mode
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	kindStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)

	// Print header
	if console {
		fmt.Printf("%s  %s\n",
			headerStyle.Render(fmt.Sprintf("%-*s", maxID, "ID")),
			headerStyle.Render(fmt.Sprintf("%-*s", maxKind, "KIND")))
		fmt.Printf("%s  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(strings.Repeat("─", maxID)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(strings.Repeat("─", maxKind)))
	} else {
		fmt.Printf("%-*s  %-*s\n", maxID, "ID", maxKind, "KIND")
		fmt.Printf("%s  %s\n", strings.Repeat("-", maxID), strings.Repeat("-", maxKind))
	}

	for _, e := range entries {
		id := e.ID.String()
		if len(id) > maxID {
			id = id[:maxID-3] + "..."
		}
		kind := e.Kind
		if len(kind) > maxKind {
			kind = kind[:maxKind-3] + "..."
		}
		if console {
			fmt.Printf("%s  %s\n",
				idStyle.Render(fmt.Sprintf("%-*s", maxID, id)),
				kindStyle.Render(fmt.Sprintf("%-*s", maxKind, kind)))
		} else {
			fmt.Printf("%-*s  %-*s\n", maxID, id, maxKind, kind)
		}
	}

	if console {
		fmt.Printf("\n%s\n", countStyle.Render(fmt.Sprintf("Total: %d entries", len(entries))))
	} else {
		fmt.Printf("\nTotal: %d entries\n", len(entries))
	}
}

func outputEntryJSON(entry *regapi.Entry) error {
	// Convert data to map for JSON output
	dataMap := extractDataMap(entry)

	output := map[string]interface{}{
		"id":   entry.ID.String(),
		"kind": entry.Kind,
		"meta": entry.Meta,
		"data": dataMap,
	}

	data, err := stdjson.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputEntryYAML(entry *regapi.Entry) error {
	dataMap := extractDataMap(entry)

	output := map[string]interface{}{
		"id":   entry.ID.String(),
		"kind": entry.Kind,
		"meta": entry.Meta,
		"data": dataMap,
	}

	data, err := yaml.Marshal(output)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func outputEntryTable(entry *regapi.Entry) {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	if console {
		fmt.Printf("%s %s\n", labelStyle.Render("ID:"), valueStyle.Render(entry.ID.String()))
		fmt.Printf("%s %s\n", labelStyle.Render("Kind:"), dimStyle.Render(entry.Kind))
	} else {
		fmt.Printf("ID:   %s\n", entry.ID.String())
		fmt.Printf("Kind: %s\n", entry.Kind)
	}

	if len(entry.Meta) > 0 {
		if console {
			fmt.Printf("\n%s\n", sectionStyle.Render("Metadata:"))
		} else {
			fmt.Println("\nMetadata:")
		}
		keys := make([]string, 0, len(entry.Meta))
		for k := range entry.Meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if console {
				fmt.Printf("  %s %v\n", keyStyle.Render(k+":"), entry.Meta[k])
			} else {
				fmt.Printf("  %s: %v\n", k, entry.Meta[k])
			}
		}
	}

	dataMap := extractDataMap(entry)
	if len(dataMap) > 0 {
		if console {
			fmt.Printf("\n%s\n", sectionStyle.Render("Data:"))
		} else {
			fmt.Println("\nData:")
		}
		// Output full data as YAML for readability
		data, err := yaml.Marshal(dataMap)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if line != "" {
					if console {
						fmt.Printf("  %s\n", dimStyle.Render(line))
					} else {
						fmt.Printf("  %s\n", line)
					}
				}
			}
		}
	}
}

func showEntryField(entry *regapi.Entry, fieldName string, jsonOutput, yamlOutput, rawOutput bool) error {
	dataMap := extractDataMap(entry)

	value, exists := dataMap[fieldName]
	if !exists {
		return fmt.Errorf("field %q not found in entry data", fieldName)
	}

	if rawOutput {
		fmt.Printf("%v", value)
		return nil
	}

	if jsonOutput {
		data, err := stdjson.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if yamlOutput {
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	}

	// Default: just print the value
	if str, ok := value.(string); ok {
		fmt.Println(str)
	} else {
		data, _ := yaml.Marshal(value)
		fmt.Print(string(data))
	}
	return nil
}

func extractDataMap(entry *regapi.Entry) map[string]interface{} {
	if entry.Data == nil {
		return nil
	}

	var dataMap map[string]interface{}
	if err := transcoder.GlobalTranscoder().Unmarshal(entry.Data, &dataMap); err != nil {
		return nil
	}
	return dataMap
}
