package cmd

import (
	"context"
	stdjson "encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	regapi "github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	luaboot "github.com/wippyai/runtime/boot/components/runtime/lua"
	bootextensions "github.com/wippyai/runtime/boot/extensions"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	_ "github.com/wippyai/runtime/runtime/lua/code/lint/rules" // register lint rules
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// ----------------------------------------------------------------------------
// Command definition
// ----------------------------------------------------------------------------

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
	lintCmd.Flags().Bool("cache-reset", false, "clear lua cache before linting")
}

// ----------------------------------------------------------------------------
// Styles
// ----------------------------------------------------------------------------

var (
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	styleWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	styleHint    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleCode    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleNS      = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	styleEntry   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleFace    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
)

// ----------------------------------------------------------------------------
// Severity helpers
// ----------------------------------------------------------------------------

type severity int

const (
	severityHint severity = iota
	severityWarning
	severityError
)

func parseSeverity(s string) severity {
	switch strings.ToLower(s) {
	case "hint":
		return severityHint
	case "warning", "warn":
		return severityWarning
	default:
		return severityError
	}
}

func (s severity) String() string {
	switch s {
	case severityHint:
		return "hint"
	case severityWarning:
		return "warning"
	default:
		return "error"
	}
}

func fromDiagSeverity(ds diag.Severity) severity {
	switch ds {
	case diag.SeverityHint:
		return severityHint
	case diag.SeverityWarning:
		return severityWarning
	default:
		return severityError
	}
}

func (s severity) style() lipgloss.Style {
	switch s {
	case severityHint:
		return styleHint
	case severityWarning:
		return styleWarning
	default:
		return styleError
	}
}

// ----------------------------------------------------------------------------
// Data types
// ----------------------------------------------------------------------------

// Diagnostic represents a single lint diagnostic for JSON output.
type Diagnostic struct {
	EntryID  string `json:"entry_id"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

// RichDiagnostic holds a diagnostic with source for rendering.
type RichDiagnostic struct {
	EntryID string
	Diag    diag.Diagnostic
	Source  diag.SourceLines
}

// LintResult holds the complete lint results.
type LintResult struct {
	Diagnostics     []Diagnostic     `json:"diagnostics"`
	RichDiagnostics []RichDiagnostic `json:"-"`
	TotalEntries    int              `json:"total_entries"`
	ErrorCount      int              `json:"error_count"`
	WarningCount    int              `json:"warning_count"`
	HintCount       int              `json:"hint_count"`
}

// entryResult holds per-entry lint output for aggregation.
type entryResult struct {
	entryID     regapi.ID
	manifest    *io.Manifest
	diagnostics []Diagnostic
	rich        []RichDiagnostic
	errors      int
	warnings    int
	hints       int
}

// entryData holds extracted source and imports from an entry.
type entryData struct {
	Imports map[string]regapi.ID
	Source  string
	Method  string
}

// lintConfig holds runtime configuration for a lint session.
type lintConfig struct {
	minSeverity severity
	workers     int
}

// luaEntryKinds are the entry kinds that contain Lua code.
var luaEntryKinds = []string{
	"function.lua",
	"library.lua",
	"process.lua",
	"workflow.lua",
}

// ----------------------------------------------------------------------------
// Main command
// ----------------------------------------------------------------------------

func runLint(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	opts, err := parseLintFlags(cmd)
	if err != nil {
		return err
	}

	ctx, loader, err := bootstrapLintContext()
	if err != nil {
		return err
	}

	if profiler {
		if err := loader.Start(ctx); err != nil {
			return fmt.Errorf("failed to start components: %w", err)
		}
		defer func() { _ = loader.Shutdown(ctx) }()
	}

	luaEntries, reportSet, err := loadLuaEntries(cmd, opts.lockFile, opts.nsFilters)
	if err != nil {
		return err
	}

	linter, lcache := createLinter(ctx, opts.enableRules)
	if opts.cacheReset {
		if err := resetLintCache(lcache); err != nil {
			return err
		}
	}
	cfg := lintConfig{
		minSeverity: opts.minSeverity,
		workers:     runtime.NumCPU(),
	}

	var result *LintResult
	if console {
		result, err = runLintWithUI(luaEntries, reportSet, linter, lcache, cfg)
	} else {
		result = runLintSimple(luaEntries, reportSet, linter, lcache, cfg)
	}
	if err != nil {
		return err
	}

	result = applyFilters(result, opts.codeFilters, opts.limit)
	return outputResults(result, opts)
}

// lintOptions holds parsed command flags.
type lintOptions struct {
	lockFile    string
	nsFilters   []string
	codeFilters []string
	minSeverity severity
	limit       int
	jsonOutput  bool
	noColor     bool
	showSummary bool
	enableRules bool
	cacheReset  bool
}

func parseLintFlags(cmd *cobra.Command) (lintOptions, error) {
	lockFile, _ := cmd.Flags().GetString("lock-file")
	level, _ := cmd.Flags().GetString("level")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	nsFilters, _ := cmd.Flags().GetStringSlice("ns")
	noColor, _ := cmd.Flags().GetBool("no-color")
	showSummary, _ := cmd.Flags().GetBool("summary")
	codeFilters, _ := cmd.Flags().GetStringSlice("code")
	limit, _ := cmd.Flags().GetInt("limit")
	enableRules, _ := cmd.Flags().GetBool("rules")
	cacheReset, _ := cmd.Flags().GetBool("cache-reset")

	return lintOptions{
		lockFile:    lockFile,
		minSeverity: parseSeverity(level),
		jsonOutput:  jsonOutput,
		nsFilters:   nsFilters,
		noColor:     noColor,
		showSummary: showSummary,
		codeFilters: codeFilters,
		limit:       limit,
		enableRules: enableRules,
		cacheReset:  cacheReset,
	}, nil
}

func bootstrapLintContext() (ctx context.Context, loader *bootpkg.Loader, err error) {
	logger, err := clilogger.CreateLogger(clilogger.Config{
		Silent:       true,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return nil, nil, NewCreateLoggerError(err)
	}

	cfg, err := loadBootConfig()
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		cfg = createDefaultConfig()
	}

	bctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		return nil, nil, NewInitializeBootstrapContextError(err)
	}

	components := StandardComponents()
	reservedNames := make(map[string]struct{}, len(components))
	for _, comp := range components {
		if comp == nil {
			continue
		}
		name := comp.Name()
		if name == "" {
			continue
		}
		reservedNames[name] = struct{}{}
	}

	bctx, extensionResult, err := bootextensions.LoadWithReserved(bctx, cfg, reservedNames)
	if err != nil {
		return nil, nil, err
	}

	components = append(components, extensionResult.Components...)
	loader, err = bootpkg.NewLoader(components...)
	if err != nil {
		return nil, nil, NewCreateLoaderError(err)
	}

	bctx, err = loader.Load(bctx)
	if err != nil {
		return nil, nil, NewLoadComponentsError(err)
	}

	return bctx, loader, nil
}

func loadLuaEntries(cmd *cobra.Command, lockFile string, nsFilters []string) ([]regapi.Entry, map[regapi.ID]bool, error) {
	logger := zap.NewNop()

	lockPath, lockObj, err := loadValidatedLock(".", lockFile)
	if err != nil {
		return nil, nil, err
	}

	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return nil, nil, NewInitAppError(err)
	}

	allEntries, err := ensureModulesAndLoadEntries(app.Ctx, lockPath, lockObj, logger, false)
	if err != nil {
		return nil, nil, err
	}

	allLua := filterLuaEntries(allEntries, nil)
	selected := filterLuaEntries(allEntries, nsFilters)
	expanded, reportSet := expandLuaEntriesByImports(allLua, selected)

	return expanded, reportSet, nil
}

func createLinter(ctx context.Context, enableRules bool) (*lint.Linter, lintCache) {
	cm := luaboot.GetCodeManager(ctx)
	var mods []*luaapi.ModuleDef
	if cm != nil {
		mods = cm.GetModuleDefs()
	}

	typeCfg := code.TypeCheckConfig{
		Enabled: true,
		Strict:  true,
	}
	typeChecker := code.NewTypeChecker(typeCfg, mods)

	var registry *lint.Registry
	if enableRules {
		registry = lint.DefaultRegistry.Clone()
	} else {
		registry = lint.NewRegistry()
	}

	lcache := lintCache{}
	if cm != nil {
		lcache.store = cm.CacheStore()
		lcache.cfg = cm.CacheConfig()
	}
	lcache.typecheckHash = code.TypecheckConfigHash(typeCfg)
	lcache.builtinModules = make([]string, 0, len(mods))
	builtinManifests := make(map[string]*io.Manifest)
	for _, mod := range mods {
		if mod == nil || mod.Types == nil {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		lcache.builtinModules = append(lcache.builtinModules, mod.Name)
		builtinManifests[mod.Name] = manifest
	}
	lcache.builtinHash = code.BuiltinManifestHash(builtinManifests)

	return lint.New(typeChecker, registry), lcache
}

func applyFilters(result *LintResult, codeFilters []string, limit int) *LintResult {
	if len(codeFilters) > 0 {
		result = filterByCode(result, codeFilters)
	}
	if limit > 0 {
		result = applyLimit(result, limit)
	}
	return result
}

func outputResults(result *LintResult, opts lintOptions) error {
	if opts.jsonOutput {
		return outputJSON(result)
	}

	if opts.showSummary {
		outputSummary(result, opts.noColor)
	} else {
		outputTable(result, opts.noColor)
	}

	if result.ErrorCount > 0 {
		return NewLintFailedError(result.ErrorCount, result.WarningCount)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Linting execution
// ----------------------------------------------------------------------------

func runLintWithUI(luaEntries []regapi.Entry, reportSet map[regapi.ID]bool, linter *lint.Linter, lcache lintCache, cfg lintConfig) (*LintResult, error) {
	prog := progress.New(progress.WithDefaultGradient())
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	m := &lintModel{
		progress:     prog,
		spinner:      s,
		totalEntries: len(luaEntries),
	}

	p := tea.NewProgram(m)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.Send(lintErrorMsg{err: fmt.Errorf("lint panic: %v", r)})
			}
		}()
		result := lintEntries(luaEntries, reportSet, linter, lcache, cfg, p)
		p.Send(lintCompleteMsg{result: result})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	if lm, ok := finalModel.(*lintModel); ok {
		if lm.err != nil {
			return nil, lm.err
		}
		if lm.result == nil {
			return nil, fmt.Errorf("lint operation was interrupted")
		}
		return lm.result, nil
	}

	return nil, fmt.Errorf("lint operation was interrupted")
}

func runLintSimple(luaEntries []regapi.Entry, reportSet map[regapi.ID]bool, linter *lint.Linter, lcache lintCache, cfg lintConfig) *LintResult {
	result := lintEntries(luaEntries, reportSet, linter, lcache, cfg, nil)
	fmt.Fprintf(os.Stderr, "\r                    \r")
	return result
}

// lintEntries is the core linting loop. If prog is non-nil, sends UI updates.
func lintEntries(luaEntries []regapi.Entry, reportSet map[regapi.ID]bool, linter *lint.Linter, lcache lintCache, cfg lintConfig, prog *tea.Program) *LintResult {
	result := &LintResult{TotalEntries: len(luaEntries)}

	levels, _ := topology.LevelSortEntriesByDependency(luaEntries, &luaImportResolver{})
	entryDataMap := make(map[regapi.ID]entryData, len(luaEntries))
	for _, entry := range luaEntries {
		entryDataMap[entry.ID] = extractEntryData(entry)
	}
	fps := computeLintFingerprints(levels, entryDataMap, lcache)
	manifestMap := make(map[regapi.ID]*io.Manifest)

	var checked, errorCount, warnCount atomic.Int64
	var lastPercent atomic.Int64
	total := int64(len(luaEntries))

	notifyProgress := func(entry string, entryIssues int) {
		n := checked.Load()
		pct := n * 100 / total

		if prog != nil {
			prog.Send(lintProgressMsg{
				percent:    float64(n) / float64(total),
				entry:      entry,
				checked:    int(n),
				errors:     int(errorCount.Load()),
				warnings:   int(warnCount.Load()),
				entryIssue: entryIssues,
			})
		} else if pct > lastPercent.Load() && pct%10 == 0 {
			fmt.Fprintf(os.Stderr, "\rLinting... %d%%", pct)
			lastPercent.Store(pct)
		}
	}

	for _, levelEntries := range levels {
		if len(levelEntries) == 0 {
			continue
		}

		if len(levelEntries) == 1 {
			entry := levelEntries[0]
			if prog != nil {
				prog.Send(lintEntryMsg{entry: entry.ID.String()})
			}

			er := lintOneEntry(entry, entryDataMap[entry.ID], linter, manifestMap, cfg.minSeverity, lcache, fps)
			checked.Add(1)

			entryIssues := 0
			if er != nil {
				if shouldReport(reportSet, entry.ID) {
					entryIssues = er.errors + er.warnings + er.hints
					errorCount.Add(int64(er.errors))
					warnCount.Add(int64(er.warnings))
				}
				mergeEntryResult(result, er, manifestMap, reportSet)
			}
			notifyProgress(entry.ID.String(), entryIssues)
			continue
		}

		results := make([]entryResult, len(levelEntries))
		sem := make(chan struct{}, cfg.workers)
		var wg sync.WaitGroup

		for i, entry := range levelEntries {
			wg.Add(1)
			sem <- struct{}{}

			go func(idx int, e regapi.Entry) {
				defer wg.Done()
				defer func() { <-sem }()

				if prog != nil {
					prog.Send(lintEntryMsg{entry: e.ID.String()})
				}

				clone := linter.Clone()
				er := lintOneEntry(e, entryDataMap[e.ID], clone, manifestMap, cfg.minSeverity, lcache, fps)
				checked.Add(1)

				entryIssues := 0
				if er != nil {
					results[idx] = *er
					if shouldReport(reportSet, e.ID) {
						entryIssues = er.errors + er.warnings + er.hints
						errorCount.Add(int64(er.errors))
						warnCount.Add(int64(er.warnings))
					}
				}
				notifyProgress(e.ID.String(), entryIssues)
			}(i, entry)
		}
		wg.Wait()

		for i := range results {
			mergeEntryResult(result, &results[i], manifestMap, reportSet)
		}
	}

	sortLintResults(result)
	return result
}

func lintOneEntry(entry regapi.Entry, data entryData, linter *lint.Linter, manifestMap map[regapi.ID]*io.Manifest, minSev severity, lcache lintCache, fps lintFingerprints) *entryResult {
	if data.Source == "" {
		return nil
	}

	entryID := entry.ID.String()
	sourceLines := diag.ParseSource(data.Source)

	stmts, parseErr := parse.ParseString(data.Source, entryID)
	if parseErr != nil {
		return &entryResult{
			entryID: entry.ID,
			diagnostics: []Diagnostic{{
				EntryID:  entryID,
				Code:     "P0001",
				Severity: "error",
				Message:  parseErr.Error(),
			}},
			errors: 1,
		}
	}

	imports := make(map[string]*io.Manifest)
	for alias, importID := range data.Imports {
		if importID.NS == "" {
			if manifest := linter.BuiltinManifest(importID.Name); manifest != nil {
				imports[alias] = manifest
			}
			continue
		}
		if manifest, ok := manifestMap[importID]; ok {
			imports[alias] = manifest
		}
	}

	var cachedManifest *io.Manifest
	var cachedDiagnostics []diag.Diagnostic
	if tcFP := fps.typecheck[entry.ID]; tcFP != "" {
		if manifest, diags, ok := lintLoadTypecheckCache(lcache, entry.ID, tcFP); ok {
			cachedManifest = manifest
			cachedDiagnostics = diags
		}
	}

	enableTypecheck := cachedDiagnostics == nil
	lintResult := linter.CheckParsedWithTypecheck(stmts, entryID, imports, enableTypecheck)
	linter.ClearCache()

	if cachedDiagnostics != nil {
		lintResult.Manifest = cachedManifest
		lintResult.Diagnostics = append(cachedDiagnostics, lintResult.Diagnostics...)
	}

	typeDiags := filterTypecheckDiagnostics(lintResult.Diagnostics)
	if lintResult.Manifest != nil {
		lintSaveTypecheckCache(lcache, entry, data, fps.typecheck[entry.ID], fps.typeDeps[entry.ID], lintResult.Manifest, typeDiags)
	}

	if fp := fps.compile[entry.ID]; fp != "" {
		if !code.HasErrors(typeDiags) {
			lintEnsureCompileCache(lcache, entry, data, fp, fps.compileDeps[entry.ID], stmts, lintResult.Manifest)
		}
	}

	er := &entryResult{
		entryID:  entry.ID,
		manifest: lintResult.Manifest,
	}

	for _, d := range lintResult.Diagnostics {
		sev := fromDiagSeverity(d.Severity)
		if sev < minSev {
			continue
		}

		er.diagnostics = append(er.diagnostics, Diagnostic{
			EntryID:  entryID,
			Code:     formatDiagCode(d.Code),
			Severity: sev.String(),
			Message:  d.Message,
			Line:     d.Position.Line,
			Column:   d.Position.Column,
		})
		er.rich = append(er.rich, RichDiagnostic{
			EntryID: entryID,
			Diag:    d,
			Source:  sourceLines,
		})

		switch sev {
		case severityError:
			er.errors++
		case severityWarning:
			er.warnings++
		case severityHint:
			er.hints++
		}
	}

	return er
}

func mergeEntryResult(result *LintResult, er *entryResult, manifestMap map[regapi.ID]*io.Manifest, reportSet map[regapi.ID]bool) {
	if er == nil {
		return
	}
	if er.manifest != nil {
		manifestMap[er.entryID] = er.manifest
	}
	if shouldReport(reportSet, er.entryID) {
		result.Diagnostics = append(result.Diagnostics, er.diagnostics...)
		result.RichDiagnostics = append(result.RichDiagnostics, er.rich...)
		result.ErrorCount += er.errors
		result.WarningCount += er.warnings
		result.HintCount += er.hints
	}
}

// ----------------------------------------------------------------------------
// Entry filtering and resolution
// ----------------------------------------------------------------------------

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
	kindStr := kind
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
		if strings.HasSuffix(filter, ".*") || strings.HasSuffix(filter, ".**") {
			prefix := strings.TrimRight(filter, ".*")
			if ns == prefix || strings.HasPrefix(ns, prefix+".") {
				return true
			}
		}
	}
	return false
}

func shouldReport(reportSet map[regapi.ID]bool, id regapi.ID) bool {
	if reportSet == nil {
		return true
	}
	return reportSet[id]
}

func expandLuaEntriesByImports(allLua []regapi.Entry, selected []regapi.Entry) ([]regapi.Entry, map[regapi.ID]bool) {
	reportSet := make(map[regapi.ID]bool, len(selected))
	for _, entry := range selected {
		reportSet[entry.ID] = true
	}

	byID := make(map[regapi.ID]regapi.Entry, len(allLua))
	for _, entry := range allLua {
		byID[entry.ID] = entry
	}

	expandedSet := make(map[regapi.ID]regapi.Entry, len(selected))
	queue := make([]regapi.Entry, 0, len(selected))
	for _, entry := range selected {
		expandedSet[entry.ID] = entry
		queue = append(queue, entry)
	}

	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]
		data := extractEntryData(entry)
		for _, importID := range data.Imports {
			if depEntry, ok := byID[importID]; ok {
				if _, seen := expandedSet[depEntry.ID]; !seen {
					expandedSet[depEntry.ID] = depEntry
					queue = append(queue, depEntry)
				}
			}
		}
	}

	expanded := make([]regapi.Entry, 0, len(expandedSet))
	for _, entry := range expandedSet {
		expanded = append(expanded, entry)
	}
	return expanded, reportSet
}

func extractEntryData(entry regapi.Entry) entryData {
	if entry.Data == nil {
		return entryData{}
	}

	// Fast path: loader entries usually carry golang map payloads.
	if m, ok := entry.Data.Data().(map[string]any); ok {
		return entryDataFromMap(m)
	}
	if m, ok := entry.Data.Data().(map[string]interface{}); ok {
		return entryDataFromMap(m)
	}

	var cfg struct {
		Source  string               `json:"source"`
		Method  string               `json:"method"`
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
	for _, mod := range cfg.Modules {
		imports[mod] = regapi.NewID("", mod)
	}

	return entryData{Source: cfg.Source, Imports: imports, Method: cfg.Method}
}

func entryDataFromMap(m map[string]any) entryData {
	if m == nil {
		return entryData{}
	}

	data := entryData{
		Imports: make(map[string]regapi.ID),
	}

	if source, ok := m["source"].(string); ok {
		data.Source = source
	}
	if method, ok := m["method"].(string); ok {
		data.Method = method
	}

	if rawImports, ok := m["imports"].(map[string]any); ok {
		for alias, raw := range rawImports {
			if id, ok := parseRegistryID(raw); ok {
				data.Imports[alias] = id
			}
		}
	}
	if rawImports, ok := m["imports"].(map[string]regapi.ID); ok {
		for alias, id := range rawImports {
			data.Imports[alias] = id
		}
	}

	if rawModules, ok := m["modules"].([]any); ok {
		for _, mod := range rawModules {
			if modName, ok := mod.(string); ok && modName != "" {
				data.Imports[modName] = regapi.NewID("", modName)
			}
		}
	}
	if rawModules, ok := m["modules"].([]string); ok {
		for _, modName := range rawModules {
			if modName != "" {
				data.Imports[modName] = regapi.NewID("", modName)
			}
		}
	}

	return data
}

func parseRegistryID(v any) (regapi.ID, bool) {
	switch typed := v.(type) {
	case regapi.ID:
		return typed, true
	case string:
		if typed == "" {
			return regapi.ID{}, false
		}
		return regapi.ParseID(typed), true
	case map[string]any:
		ns, _ := typed["ns"].(string)
		name, _ := typed["name"].(string)
		if name == "" {
			return regapi.ID{}, false
		}
		return regapi.NewID(ns, name), true
	default:
		return regapi.ID{}, false
	}
}

// luaImportResolver extracts Lua import dependencies from entries.
type luaImportResolver struct{}

func (r *luaImportResolver) Extract(entry regapi.Entry) []string {
	data := extractEntryData(entry)
	deps := make([]string, 0, len(data.Imports))
	for _, importID := range data.Imports {
		deps = append(deps, importID.String())
	}
	return deps
}

func (r *luaImportResolver) RegisterPattern(_ regapi.DependencyPattern) error {
	return nil
}

// ----------------------------------------------------------------------------
// Result formatting
// ----------------------------------------------------------------------------

func formatDiagCode(code diag.Code) string {
	if code >= lint.LintCodeBase {
		return lint.FormatLintCode(code)
	}
	return code.Name()
}

func renderRichDiag(rd RichDiagnostic, noColor bool) string {
	var rendered string
	if noColor {
		rendered = rd.Diag.Render(rd.Source)
	} else {
		rendered = rd.Diag.RenderColored(rd.Source)
	}
	if rd.Diag.Code >= lint.LintCodeBase {
		rendered = strings.Replace(rendered, rd.Diag.Code.Name(), lint.FormatLintCode(rd.Diag.Code), 1)
	}
	return rendered
}

func sortLintResults(result *LintResult) {
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		a, b := result.Diagnostics[i], result.Diagnostics[j]
		if a.EntryID != b.EntryID {
			return a.EntryID < b.EntryID
		}
		if a.Severity != b.Severity {
			return parseSeverity(a.Severity) > parseSeverity(b.Severity)
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})

	sort.Slice(result.RichDiagnostics, func(i, j int) bool {
		a, b := result.RichDiagnostics[i], result.RichDiagnostics[j]
		if a.EntryID != b.EntryID {
			return a.EntryID < b.EntryID
		}
		if a.Diag.Severity != b.Diag.Severity {
			return fromDiagSeverity(a.Diag.Severity) > fromDiagSeverity(b.Diag.Severity)
		}
		if a.Diag.Position.Line != b.Diag.Position.Line {
			return a.Diag.Position.Line < b.Diag.Position.Line
		}
		if a.Diag.Position.Column != b.Diag.Position.Column {
			return a.Diag.Position.Column < b.Diag.Position.Column
		}
		if a.Diag.Code != b.Diag.Code {
			return a.Diag.Code < b.Diag.Code
		}
		return a.Diag.Message < b.Diag.Message
	})
}

func filterByCode(result *LintResult, codes []string) *LintResult {
	codeSet := make(map[string]bool, len(codes))
	for _, c := range codes {
		codeSet[strings.ToUpper(c)] = true
	}

	filtered := &LintResult{TotalEntries: result.TotalEntries}

	for _, d := range result.Diagnostics {
		if codeSet[strings.ToUpper(d.Code)] {
			filtered.Diagnostics = append(filtered.Diagnostics, d)
			switch parseSeverity(d.Severity) {
			case severityError:
				filtered.ErrorCount++
			case severityWarning:
				filtered.WarningCount++
			case severityHint:
				filtered.HintCount++
			}
		}
	}

	for _, rd := range result.RichDiagnostics {
		if codeSet[strings.ToUpper(formatDiagCode(rd.Diag.Code))] {
			filtered.RichDiagnostics = append(filtered.RichDiagnostics, rd)
		}
	}

	return filtered
}

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

// ----------------------------------------------------------------------------
// Output formatters
// ----------------------------------------------------------------------------

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
		lintPrintSuccess("No issues found", noColor)
		fmt.Printf("Checked %d entries\n", result.TotalEntries)
		return
	}

	for _, rd := range result.RichDiagnostics {
		fmt.Println(renderRichDiag(rd, noColor))
		fmt.Println()
	}

	printSummaryLine(result, noColor)
}

func outputSummary(result *LintResult, noColor bool) {
	if len(result.Diagnostics) == 0 {
		lintPrintSuccess("No issues found", noColor)
		fmt.Printf("Checked %d entries\n", result.TotalEntries)
		return
	}

	byCode, byNS := groupDiagnostics(result.Diagnostics)

	fmt.Printf("\nBy namespace:\n\n")
	for _, s := range byNS {
		total := s.errors + s.warnings + s.hints
		parts := formatIssueCounts(s.errors, s.warnings, s.hints)
		if noColor {
			fmt.Printf("  %-30s %d issues (%s)\n", s.ns, total, strings.Join(parts, ", "))
		} else {
			fmt.Printf("  %-30s %d issues (%s)\n", styleNS.Render(s.ns), total, strings.Join(parts, ", "))
		}
	}

	fmt.Printf("\nBy error code:\n\n")
	for _, s := range byCode {
		sev := parseSeverity(s.severity)
		if noColor {
			fmt.Printf("  %-8s [%-7s] %4d occurrences\n", s.code, s.severity, s.count)
		} else {
			fmt.Printf("  %-8s [%s] %4d occurrences\n",
				styleCode.Render(s.code),
				sev.style().Render(fmt.Sprintf("%-7s", s.severity)),
				s.count)
		}
	}

	fmt.Println()
	printSummaryLine(result, noColor)
}

func lintPrintSuccess(msg string, noColor bool) {
	if noColor {
		fmt.Println(msg)
	} else {
		fmt.Println(styleSuccess.Render(msg))
	}
}

func printSummaryLine(result *LintResult, noColor bool) {
	parts := formatResultCounts(result, noColor)
	fmt.Printf("Checked %d entries: %s\n", result.TotalEntries, strings.Join(parts, ", "))
}

func formatResultCounts(result *LintResult, noColor bool) []string {
	var parts []string
	if result.ErrorCount > 0 {
		s := fmt.Sprintf("%d errors", result.ErrorCount)
		if noColor {
			parts = append(parts, s)
		} else {
			parts = append(parts, styleError.Render(s))
		}
	}
	if result.WarningCount > 0 {
		s := fmt.Sprintf("%d warnings", result.WarningCount)
		if noColor {
			parts = append(parts, s)
		} else {
			parts = append(parts, styleWarning.Render(s))
		}
	}
	if result.HintCount > 0 {
		s := fmt.Sprintf("%d hints", result.HintCount)
		if noColor {
			parts = append(parts, s)
		} else {
			parts = append(parts, styleHint.Render(s))
		}
	}
	return parts
}

func formatIssueCounts(errors, warnings, hints int) []string {
	var parts []string
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", warnings))
	}
	if hints > 0 {
		parts = append(parts, fmt.Sprintf("%d hints", hints))
	}
	return parts
}

type codeStats struct {
	code     string
	severity string
	count    int
}

type nsStats struct {
	ns       string
	errors   int
	warnings int
	hints    int
}

func groupDiagnostics(diagnostics []Diagnostic) ([]*codeStats, []*nsStats) {
	byCode := make(map[string]*codeStats)
	byNS := make(map[string]*nsStats)

	for _, d := range diagnostics {
		if s, ok := byCode[d.Code]; ok {
			s.count++
		} else {
			byCode[d.Code] = &codeStats{code: d.Code, count: 1, severity: d.Severity}
		}

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

	codes := make([]*codeStats, 0, len(byCode))
	for _, s := range byCode {
		codes = append(codes, s)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i].count > codes[j].count })

	namespaces := make([]*nsStats, 0, len(byNS))
	for _, s := range byNS {
		namespaces = append(namespaces, s)
	}
	sort.Slice(namespaces, func(i, j int) bool {
		return (namespaces[i].errors + namespaces[i].warnings + namespaces[i].hints) >
			(namespaces[j].errors + namespaces[j].warnings + namespaces[j].hints)
	})

	return codes, namespaces
}

// ----------------------------------------------------------------------------
// Bubbletea UI model
// ----------------------------------------------------------------------------

type lintModel struct {
	err          error
	result       *LintResult
	currentEntry string
	spinner      spinner.Model
	progress     progress.Model
	checked      int
	totalEntries int
	errors       int
	warnings     int
	entryIssues  int
	percent      float64
	eyeFrame     int
	done         bool
}

type lintProgressMsg struct {
	entry      string
	percent    float64
	checked    int
	errors     int
	warnings   int
	entryIssue int
}

type lintEntryMsg struct {
	entry string
}

type lintCompleteMsg struct {
	result *LintResult
}

type lintErrorMsg struct {
	err error
}

func (m *lintModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *lintModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case lintEntryMsg:
		m.currentEntry = msg.entry
		m.entryIssues = 0
		return m, nil

	case lintProgressMsg:
		m.percent = msg.percent
		m.currentEntry = msg.entry
		m.checked = msg.checked
		m.errors = msg.errors
		m.warnings = msg.warnings
		m.entryIssues = msg.entryIssue
		return m, nil

	case lintCompleteMsg:
		m.result = msg.result
		m.percent = 1.0
		m.done = true
		return m, tea.Quit

	case lintErrorMsg:
		m.err = msg.err
		m.done = true
		return m, tea.Quit

	case spinner.TickMsg:
		m.eyeFrame++
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *lintModel) View() string {
	pad := "  "

	if m.done && m.err != nil {
		return styleError.Render(fmt.Sprintf("\n%sError: %v\n", pad, m.err))
	}

	if m.done && m.result != nil {
		return ""
	}

	var view strings.Builder
	view.WriteString("\n")
	view.WriteString(pad)
	view.WriteString(m.progress.ViewAs(m.percent))
	view.WriteString("\n\n")

	view.WriteString(pad)
	if m.entryIssues == 0 {
		view.WriteString(styleFace.Render(lintEyes(m.eyeFrame)))
	} else {
		view.WriteString(styleFace.Render(lintFace(m.entryIssues)))
	}
	view.WriteString(" ")
	if m.currentEntry != "" {
		view.WriteString(styleEntry.Render(m.currentEntry))
	}
	view.WriteString("\n")

	view.WriteString(pad)
	view.WriteString(styleMuted.Render(fmt.Sprintf("%d/%d entries", m.checked, m.totalEntries)))
	if m.errors > 0 {
		view.WriteString("  ")
		view.WriteString(styleError.Render(fmt.Sprintf("%d errors", m.errors)))
	}
	if m.warnings > 0 {
		view.WriteString("  ")
		view.WriteString(styleWarning.Render(fmt.Sprintf("%d warnings", m.warnings)))
	}
	view.WriteString("\n\n")

	return view.String()
}

func lintEyes(frame int) string {
	eyes := [][2]string{
		{"\u25d5", "\u25d5"},
		{"\u25d4", "\u25d4"},
		{"\u25d1", "\u25d0"},
		{"\u25d4", "\u25d4"},
		{"\u25d5", "\u25d5"},
		{"\u25cf", "\u25cf"},
		{"-", "-"},
		{"\u25cf", "\u25cf"},
	}
	e := eyes[frame%len(eyes)]
	return "( " + e[0] + "_" + e[1] + " )"
}

func lintFace(issues int) string {
	switch {
	case issues == 1:
		return "( ._. )"
	case issues <= 3:
		return "( \u25d4_\u25d4 )"
	case issues <= 10:
		return "( \u25c9_\u25c9 )"
	case issues <= 20:
		return "( O_o )"
	case issues <= 35:
		return "( T_T )"
	case issues <= 50:
		return "( >_< )"
	default:
		return "( x_x )"
	}
}
