package cmd

import (
	stdjson "encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	luaboot "github.com/wippyai/runtime/boot/components/runtime/lua"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/entries"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	_ "github.com/wippyai/runtime/runtime/lua/code/lint/rules"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/yuin/gopher-lua/compiler/parse"
	"github.com/yuin/gopher-lua/types/diag"
	"github.com/yuin/gopher-lua/types/io"
	"go.uber.org/zap"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Check Lua code for errors and warnings",
	Long: `Lint validates all Lua code entries without running the application.

Performs parse checking and type checking on all Lua entries:
  - function.lua.*
  - library.lua.*
  - process.lua.*
  - workflow.lua

Examples:
  wippy lint                    # Lint with default settings
  wippy lint --level warning    # Show warnings and errors
  wippy lint --level hint       # Show all diagnostics
  wippy lint --json             # Output in JSON format
  wippy lint --rules            # Enable lint rules (style warnings)`,
	RunE: runLint,
}

func init() {
	rootCmd.AddCommand(lintCmd)

	lintCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	lintCmd.Flags().String("level", "warning", "minimum severity level to report (error, warning, hint)")
	lintCmd.Flags().Bool("json", false, "output in JSON format")
	lintCmd.Flags().StringSlice("ns", nil, "filter by namespace patterns (e.g., app, lib.*)")
	lintCmd.Flags().Bool("no-color", false, "disable colored output")
	lintCmd.Flags().Bool("summary", false, "show summary grouped by error code")
	lintCmd.Flags().StringSlice("code", nil, "filter by error codes (e.g., E0001, E0004)")
	lintCmd.Flags().Int("limit", 0, "limit number of diagnostics shown (0 = unlimited)")
	lintCmd.Flags().Bool("rules", false, "enable lint rules (style and quality warnings)")
}

// Diagnostic represents a single lint diagnostic for JSON output
type Diagnostic struct {
	EntryID  string `json:"entry_id"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

// RichDiagnostic holds a diagnostic with source for rendering
type RichDiagnostic struct {
	EntryID string
	Diag    diag.Diagnostic
	Source  diag.SourceLines
}

// LintResult holds the complete lint results
type LintResult struct {
	Diagnostics     []Diagnostic     `json:"diagnostics"`
	RichDiagnostics []RichDiagnostic `json:"-"`
	TotalEntries    int              `json:"total_entries"`
	ErrorCount      int              `json:"error_count"`
	WarningCount    int              `json:"warning_count"`
	HintCount       int              `json:"hint_count"`
}

// luaEntryKinds are the entry kinds that contain Lua code
var luaEntryKinds = []string{
	"function.lua",
	"library.lua",
	"process.lua",
	"workflow.lua",
}

// lintModel is the bubbletea model for lint progress
type lintModel struct {
	progress     progress.Model
	percent      float64
	status       string
	currentEntry string
	totalEntries int
	checked      int
	result       *LintResult
	err          error
	done         bool
}

type lintProgressMsg struct {
	percent float64
	status  string
	entry   string
	checked int
}

type lintCompleteMsg struct {
	result *LintResult
}

type lintErrorMsg struct {
	err error
}

func (m *lintModel) Init() tea.Cmd {
	return nil
}

func (m *lintModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case lintProgressMsg:
		m.percent = msg.percent
		m.status = msg.status
		m.currentEntry = msg.entry
		m.checked = msg.checked
		return m, m.progress.SetPercent(msg.percent)

	case lintCompleteMsg:
		m.result = msg.result
		m.percent = 1.0
		m.done = true
		return m, tea.Sequence(m.progress.SetPercent(1.0), tea.Quit)

	case lintErrorMsg:
		m.err = msg.err
		m.done = true
		return m, tea.Quit

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

func (m *lintModel) View() string {
	if m.done && m.err != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Render(fmt.Sprintf("\n  Error: %v\n", m.err))
	}

	if m.done && m.result != nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	entryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("14"))

	var view strings.Builder
	view.WriteString("\n")
	view.WriteString(titleStyle.Render("Linting Lua entries"))
	view.WriteString("\n\n")

	view.WriteString("  ")
	view.WriteString(m.progress.View())
	view.WriteString("\n\n")

	if m.currentEntry != "" {
		view.WriteString("  ")
		view.WriteString(entryStyle.Render(m.currentEntry))
		view.WriteString("\n")
	}

	view.WriteString("  ")
	view.WriteString(statusStyle.Render(fmt.Sprintf("%d/%d entries", m.checked, m.totalEntries)))
	view.WriteString("\n\n")

	return view.String()
}

func runLint(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	lockFile, _ := cmd.Flags().GetString("lock-file")
	level, _ := cmd.Flags().GetString("level")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	nsFilters, _ := cmd.Flags().GetStringSlice("ns")
	noColor, _ := cmd.Flags().GetBool("no-color")
	showSummary, _ := cmd.Flags().GetBool("summary")
	codeFilters, _ := cmd.Flags().GetStringSlice("code")
	limit, _ := cmd.Flags().GetInt("limit")
	enableRules, _ := cmd.Flags().GetBool("rules")

	minSeverity := parseSeverityLevel(level)

	// Create logger
	logger, err := clilogger.CreateLogger(clilogger.Config{
		Silent:       true,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	// Load config
	cfg, err := loadBootConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = createDefaultConfig()
	}

	// Bootstrap context with infrastructure
	ctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		return NewInitializeBootstrapContextError(err)
	}

	// Load components to register modules
	components := StandardComponents()
	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		return NewCreateLoaderError(err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		return NewLoadComponentsError(err)
	}

	// Start profiler if enabled
	if profiler {
		err = loader.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start components: %w", err)
		}
		defer func() { _ = loader.Shutdown(ctx) }()
	}

	// Get code manager with registered modules
	cm := luaboot.GetCodeManager(ctx)
	if cm == nil {
		return fmt.Errorf("code manager not available")
	}

	logger = logapi.GetLogger(ctx).Named("lint")

	// Find and validate lock file
	lockPath, err := lock.Find(".", lockFile)
	if err != nil {
		return NewLockFileNotFoundError(err)
	}

	// Use appinit for entry loading
	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	if err := entries.EnsureModulesInstalled(app.Ctx, lockPath, logger); err != nil {
		return NewEnsureModulesInstalledError(err)
	}

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(err)
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
			return NewLoadEntriesError(path, err)
		}
		allEntries = append(allEntries, pathEntries...)
	}

	luaEntries := filterLuaEntries(allEntries, nsFilters)
	logger.Debug("found lua entries", zap.Int("count", len(luaEntries)))

	// Create type checker and linter with modules from code manager
	mods := cm.GetModuleDefs()
	typeChecker := code.NewTypeChecker(code.TypeCheckConfig{
		Enabled: true,
		Strict:  true,
	}, mods)

	// Configure lint registry
	var registry *lint.Registry
	if enableRules {
		registry = lint.DefaultRegistry.Clone()
	} else {
		registry = lint.NewRegistry()
	}
	linter := lint.New(typeChecker, registry)

	var result *LintResult

	if console {
		// Console mode: use bubbletea progress bar
		prog := progress.New(progress.WithDefaultGradient())
		m := &lintModel{
			progress:     prog,
			status:       "Initializing...",
			totalEntries: len(luaEntries),
		}

		p := tea.NewProgram(m)

		go func() {
			res := lintEntriesWithProgress(luaEntries, linter, minSeverity, p)
			p.Send(lintCompleteMsg{result: res})
		}()

		finalModel, err := p.Run()
		if err != nil {
			return err
		}

		if lintModel, ok := finalModel.(*lintModel); ok {
			if lintModel.err != nil {
				return lintModel.err
			}
			result = lintModel.result
		}
	} else {
		// Non-console mode: simple progress output
		result = lintEntriesWithSimpleProgress(luaEntries, linter, minSeverity)
	}

	// Filter by error codes if specified
	if len(codeFilters) > 0 {
		result = filterByCode(result, codeFilters)
	}

	// Apply limit if specified
	if limit > 0 {
		result = applyLimit(result, limit)
	}

	if jsonOutput {
		return outputJSON(result)
	}

	if showSummary {
		outputSummary(result, noColor)
	} else {
		outputTable(result, noColor)
	}

	if result.ErrorCount > 0 {
		return NewLintFailedError(result.ErrorCount, result.WarningCount)
	}

	return nil
}

func parseSeverityLevel(level string) int {
	switch strings.ToLower(level) {
	case "hint":
		return 0
	case "warning", "warn":
		return 1
	case "error", "err":
		return 2
	default:
		return 2
	}
}

func severityToInt(severity diag.Severity) int {
	switch severity {
	case diag.SeverityHint:
		return 0
	case diag.SeverityWarning:
		return 1
	case diag.SeverityError:
		return 2
	default:
		return 0
	}
}

func severityToString(severity diag.Severity) string {
	switch severity {
	case diag.SeverityHint:
		return "hint"
	case diag.SeverityWarning:
		return "warning"
	case diag.SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

func filterLuaEntries(entries []regapi.Entry, nsFilters []string) []regapi.Entry {
	var result []regapi.Entry

	for _, entry := range entries {
		if !isLuaEntry(entry.Kind) {
			continue
		}

		if len(nsFilters) > 0 && !matchesNSFilter(entry.ID.NS, nsFilters) {
			continue
		}

		result = append(result, entry)
	}

	return result
}

func isLuaEntry(kind regapi.Kind) bool {
	kindStr := string(kind)
	for _, prefix := range luaEntryKinds {
		if strings.HasPrefix(kindStr, prefix) {
			return true
		}
	}
	return false
}

func matchesNSFilter(ns string, filters []string) bool {
	for _, filter := range filters {
		if filter == ns {
			return true
		}
		if strings.HasSuffix(filter, ".*") {
			prefix := strings.TrimSuffix(filter, ".*")
			if ns == prefix || strings.HasPrefix(ns, prefix+".") {
				return true
			}
		}
		if strings.HasSuffix(filter, ".**") {
			prefix := strings.TrimSuffix(filter, ".**")
			if ns == prefix || strings.HasPrefix(ns, prefix+".") {
				return true
			}
		}
	}
	return false
}

func lintEntriesWithProgress(luaEntries []regapi.Entry, linter *lint.Linter, minSeverity int, p *tea.Program) *LintResult {
	result := &LintResult{
		TotalEntries: len(luaEntries),
	}

	levels, _ := topology.LevelSortEntriesByDependency(luaEntries, &luaImportResolver{})
	manifestMap := make(map[regapi.ID]*io.Manifest)

	checked := 0
	total := len(luaEntries)

	for _, levelEntries := range levels {
		for _, entry := range levelEntries {
			data := extractEntryData(entry)
			if data.Source == "" {
				checked++
				continue
			}

			entryID := entry.ID.String()
			sourceLines := diag.ParseSource(data.Source)

			p.Send(lintProgressMsg{
				percent: float64(checked) / float64(total),
				status:  "Checking...",
				entry:   entryID,
				checked: checked,
			})

			_, parseErr := parse.ParseString(data.Source, entryID)
			if parseErr != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					EntryID:  entryID,
					Code:     "P0001",
					Severity: "error",
					Message:  parseErr.Error(),
				})
				result.ErrorCount++
				checked++
				continue
			}

			imports := make(map[string]*io.Manifest)
			for alias, importID := range data.Imports {
				if importID.NS == "" {
					continue
				}
				if manifest, ok := manifestMap[importID]; ok {
					imports[alias] = manifest
				}
			}

			lintResult := linter.Check(data.Source, entryID, imports)
			if lintResult.Manifest != nil {
				manifestMap[entry.ID] = lintResult.Manifest
			}

			for _, d := range lintResult.Diagnostics {
				sevInt := severityToInt(d.Severity)
				if sevInt < minSeverity {
					continue
				}

				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					EntryID:  entryID,
					Code:     d.Code.Name(),
					Severity: severityToString(d.Severity),
					Message:  d.Message,
					Line:     d.Position.Line,
					Column:   d.Position.Column,
				})

				result.RichDiagnostics = append(result.RichDiagnostics, RichDiagnostic{
					EntryID: entryID,
					Diag:    d,
					Source:  sourceLines,
				})

				switch d.Severity {
				case diag.SeverityError:
					result.ErrorCount++
				case diag.SeverityWarning:
					result.WarningCount++
				case diag.SeverityHint:
					result.HintCount++
				}
			}

			checked++
		}
	}

	sortLintResults(result)
	return result
}

func lintEntriesWithSimpleProgress(luaEntries []regapi.Entry, linter *lint.Linter, minSeverity int) *LintResult {
	result := &LintResult{
		TotalEntries: len(luaEntries),
	}

	levels, _ := topology.LevelSortEntriesByDependency(luaEntries, &luaImportResolver{})
	manifestMap := make(map[regapi.ID]*io.Manifest)

	checked := 0
	total := len(luaEntries)
	lastPercent := 0

	for _, levelEntries := range levels {
		for _, entry := range levelEntries {
			data := extractEntryData(entry)
			if data.Source == "" {
				checked++
				continue
			}

			entryID := entry.ID.String()
			sourceLines := diag.ParseSource(data.Source)

			// Simple progress output
			percent := (checked * 100) / total
			if percent > lastPercent && percent%10 == 0 {
				fmt.Fprintf(os.Stderr, "\rLinting... %d%%", percent)
				lastPercent = percent
			}

			_, parseErr := parse.ParseString(data.Source, entryID)
			if parseErr != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					EntryID:  entryID,
					Code:     "P0001",
					Severity: "error",
					Message:  parseErr.Error(),
				})
				result.ErrorCount++
				checked++
				continue
			}

			imports := make(map[string]*io.Manifest)
			for alias, importID := range data.Imports {
				if importID.NS == "" {
					continue
				}
				if manifest, ok := manifestMap[importID]; ok {
					imports[alias] = manifest
				}
			}

			lintResult := linter.Check(data.Source, entryID, imports)
			if lintResult.Manifest != nil {
				manifestMap[entry.ID] = lintResult.Manifest
			}

			for _, d := range lintResult.Diagnostics {
				sevInt := severityToInt(d.Severity)
				if sevInt < minSeverity {
					continue
				}

				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					EntryID:  entryID,
					Code:     d.Code.Name(),
					Severity: severityToString(d.Severity),
					Message:  d.Message,
					Line:     d.Position.Line,
					Column:   d.Position.Column,
				})

				result.RichDiagnostics = append(result.RichDiagnostics, RichDiagnostic{
					EntryID: entryID,
					Diag:    d,
					Source:  sourceLines,
				})

				switch d.Severity {
				case diag.SeverityError:
					result.ErrorCount++
				case diag.SeverityWarning:
					result.WarningCount++
				case diag.SeverityHint:
					result.HintCount++
				}
			}

			checked++
		}
	}

	fmt.Fprintf(os.Stderr, "\r                    \r")
	sortLintResults(result)
	return result
}

func sortLintResults(result *LintResult) {
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].EntryID != result.Diagnostics[j].EntryID {
			return result.Diagnostics[i].EntryID < result.Diagnostics[j].EntryID
		}
		return severityOrder(result.Diagnostics[i].Severity) < severityOrder(result.Diagnostics[j].Severity)
	})

	sort.Slice(result.RichDiagnostics, func(i, j int) bool {
		if result.RichDiagnostics[i].EntryID != result.RichDiagnostics[j].EntryID {
			return result.RichDiagnostics[i].EntryID < result.RichDiagnostics[j].EntryID
		}
		return severityOrder(severityToString(result.RichDiagnostics[i].Diag.Severity)) <
			severityOrder(severityToString(result.RichDiagnostics[j].Diag.Severity))
	})
}

func severityOrder(s string) int {
	switch s {
	case "error":
		return 0
	case "warning":
		return 1
	case "hint":
		return 2
	default:
		return 3
	}
}

// luaImportResolver extracts Lua import dependencies from entries
type luaImportResolver struct{}

func (r *luaImportResolver) Extract(entry regapi.Entry) []string {
	data := extractEntryData(entry)
	var deps []string
	for _, importID := range data.Imports {
		deps = append(deps, importID.String())
	}
	return deps
}

func (r *luaImportResolver) RegisterPattern(_ regapi.DependencyPattern) error {
	return nil
}

// entryData holds extracted source and imports from an entry
type entryData struct {
	Source  string
	Imports map[string]regapi.ID
}

func extractEntryData(entry regapi.Entry) entryData {
	if entry.Data == nil {
		return entryData{}
	}

	var cfg struct {
		Source  string               `json:"source"`
		Imports map[string]regapi.ID `json:"imports,omitempty"`
		Modules []string             `json:"modules,omitempty"`
	}

	if err := transcoder.GlobalTranscoder().Unmarshal(entry.Data, &cfg); err != nil {
		return entryData{}
	}

	imports := cfg.Imports
	if imports == nil {
		imports = make(map[string]regapi.ID)
	}

	// Convert modules shortcut to imports
	for _, mod := range cfg.Modules {
		imports[mod] = regapi.NewID("", mod)
	}

	return entryData{
		Source:  cfg.Source,
		Imports: imports,
	}
}

func outputJSON(result *LintResult) error {
	data, err := stdjson.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputTable(result *LintResult, noColor bool) {
	if len(result.RichDiagnostics) == 0 {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
		if noColor {
			fmt.Println("No issues found")
		} else {
			fmt.Println(style.Render("No issues found"))
		}
		fmt.Printf("Checked %d entries\n", result.TotalEntries)
		return
	}

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))

	for _, rd := range result.RichDiagnostics {
		if noColor {
			fmt.Println(rd.Diag.Render(rd.Source))
		} else {
			fmt.Println(rd.Diag.RenderColored(rd.Source))
		}
		fmt.Println()
	}

	summary := fmt.Sprintf("Checked %d entries: ", result.TotalEntries)

	var parts []string
	if result.ErrorCount > 0 {
		if noColor {
			parts = append(parts, fmt.Sprintf("%d errors", result.ErrorCount))
		} else {
			parts = append(parts, errorStyle.Render(fmt.Sprintf("%d errors", result.ErrorCount)))
		}
	}
	if result.WarningCount > 0 {
		if noColor {
			parts = append(parts, fmt.Sprintf("%d warnings", result.WarningCount))
		} else {
			parts = append(parts, warnStyle.Render(fmt.Sprintf("%d warnings", result.WarningCount)))
		}
	}
	if result.HintCount > 0 {
		if noColor {
			parts = append(parts, fmt.Sprintf("%d hints", result.HintCount))
		} else {
			parts = append(parts, hintStyle.Render(fmt.Sprintf("%d hints", result.HintCount)))
		}
	}

	fmt.Printf("%s%s\n", summary, strings.Join(parts, ", "))
}

// filterByCode filters diagnostics by error codes.
func filterByCode(result *LintResult, codes []string) *LintResult {
	codeSet := make(map[string]bool)
	for _, c := range codes {
		codeSet[strings.ToUpper(c)] = true
	}

	filtered := &LintResult{
		TotalEntries: result.TotalEntries,
	}

	for _, d := range result.Diagnostics {
		if codeSet[strings.ToUpper(d.Code)] {
			filtered.Diagnostics = append(filtered.Diagnostics, d)
			switch d.Severity {
			case "error":
				filtered.ErrorCount++
			case "warning":
				filtered.WarningCount++
			case "hint":
				filtered.HintCount++
			}
		}
	}

	for _, rd := range result.RichDiagnostics {
		if codeSet[strings.ToUpper(rd.Diag.Code.Name())] {
			filtered.RichDiagnostics = append(filtered.RichDiagnostics, rd)
		}
	}

	return filtered
}

// applyLimit limits the number of diagnostics shown.
func applyLimit(result *LintResult, limit int) *LintResult {
	if len(result.Diagnostics) <= limit {
		return result
	}

	limited := &LintResult{
		TotalEntries:    result.TotalEntries,
		Diagnostics:     result.Diagnostics[:limit],
		RichDiagnostics: result.RichDiagnostics,
		ErrorCount:      result.ErrorCount,
		WarningCount:    result.WarningCount,
		HintCount:       result.HintCount,
	}

	if len(result.RichDiagnostics) > limit {
		limited.RichDiagnostics = result.RichDiagnostics[:limit]
	}

	return limited
}

// outputSummary shows diagnostics grouped by error code and namespace.
func outputSummary(result *LintResult, noColor bool) {
	if len(result.Diagnostics) == 0 {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
		if noColor {
			fmt.Println("No issues found")
		} else {
			fmt.Println(style.Render("No issues found"))
		}
		fmt.Printf("Checked %d entries\n", result.TotalEntries)
		return
	}

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	nsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)

	// Group by code
	type codeStats struct {
		code     string
		count    int
		severity string
		example  string
	}
	byCode := make(map[string]*codeStats)

	// Group by namespace
	type nsStats struct {
		ns       string
		errors   int
		warnings int
		hints    int
	}
	byNS := make(map[string]*nsStats)

	for _, d := range result.Diagnostics {
		// By code
		if s, ok := byCode[d.Code]; ok {
			s.count++
		} else {
			byCode[d.Code] = &codeStats{
				code:     d.Code,
				count:    1,
				severity: d.Severity,
				example:  d.Message,
			}
		}

		// By namespace - extract ns from entryID (format: "ns:name")
		ns := "unknown"
		if idx := strings.Index(d.EntryID, ":"); idx > 0 {
			ns = d.EntryID[:idx]
		}
		if s, ok := byNS[ns]; ok {
			switch d.Severity {
			case "error":
				s.errors++
			case "warning":
				s.warnings++
			case "hint":
				s.hints++
			}
		} else {
			s := &nsStats{ns: ns}
			switch d.Severity {
			case "error":
				s.errors = 1
			case "warning":
				s.warnings = 1
			case "hint":
				s.hints = 1
			}
			byNS[ns] = s
		}
	}

	// Sort codes by count descending
	codes := make([]*codeStats, 0, len(byCode))
	for _, s := range byCode {
		codes = append(codes, s)
	}
	sort.Slice(codes, func(i, j int) bool {
		return codes[i].count > codes[j].count
	})

	// Sort namespaces by total issues descending
	namespaces := make([]*nsStats, 0, len(byNS))
	for _, s := range byNS {
		namespaces = append(namespaces, s)
	}
	sort.Slice(namespaces, func(i, j int) bool {
		totalI := namespaces[i].errors + namespaces[i].warnings + namespaces[i].hints
		totalJ := namespaces[j].errors + namespaces[j].warnings + namespaces[j].hints
		return totalI > totalJ
	})

	// Print by namespace
	fmt.Printf("\nBy namespace:\n\n")
	for _, s := range namespaces {
		total := s.errors + s.warnings + s.hints
		var parts []string
		if s.errors > 0 {
			parts = append(parts, fmt.Sprintf("%d errors", s.errors))
		}
		if s.warnings > 0 {
			parts = append(parts, fmt.Sprintf("%d warnings", s.warnings))
		}
		if s.hints > 0 {
			parts = append(parts, fmt.Sprintf("%d hints", s.hints))
		}
		if noColor {
			fmt.Printf("  %-30s %d issues (%s)\n", s.ns, total, strings.Join(parts, ", "))
		} else {
			fmt.Printf("  %-30s %d issues (%s)\n", nsStyle.Render(s.ns), total, strings.Join(parts, ", "))
		}
	}

	// Print by code
	fmt.Printf("\nBy error code:\n\n")
	for _, s := range codes {
		var sevStyle lipgloss.Style
		switch s.severity {
		case "error":
			sevStyle = errorStyle
		case "warning":
			sevStyle = warnStyle
		default:
			sevStyle = hintStyle
		}

		if noColor {
			fmt.Printf("  %-8s [%-7s] %4d occurrences\n", s.code, s.severity, s.count)
		} else {
			fmt.Printf("  %-8s [%s] %4d occurrences\n",
				codeStyle.Render(s.code),
				sevStyle.Render(fmt.Sprintf("%-7s", s.severity)),
				s.count)
		}
	}

	// Summary
	fmt.Println()
	summary := fmt.Sprintf("Checked %d entries: ", result.TotalEntries)
	var parts []string
	if result.ErrorCount > 0 {
		if noColor {
			parts = append(parts, fmt.Sprintf("%d errors", result.ErrorCount))
		} else {
			parts = append(parts, errorStyle.Render(fmt.Sprintf("%d errors", result.ErrorCount)))
		}
	}
	if result.WarningCount > 0 {
		if noColor {
			parts = append(parts, fmt.Sprintf("%d warnings", result.WarningCount))
		} else {
			parts = append(parts, warnStyle.Render(fmt.Sprintf("%d warnings", result.WarningCount)))
		}
	}
	if result.HintCount > 0 {
		if noColor {
			parts = append(parts, fmt.Sprintf("%d hints", result.HintCount))
		} else {
			parts = append(parts, hintStyle.Render(fmt.Sprintf("%d hints", result.HintCount)))
		}
	}
	fmt.Printf("%s%s\n", summary, strings.Join(parts, ", "))
}
