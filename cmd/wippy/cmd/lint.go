// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	luaboot "github.com/wippyai/runtime/boot/components/runtime/lua"
	"github.com/wippyai/runtime/boot/deps/lock"
	bootextensions "github.com/wippyai/runtime/boot/extensions"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/internal/entries"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	_ "github.com/wippyai/runtime/runtime/lua/code/lint/rules" // register lint rules
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
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
	lintCmd.Flags().Bool("strict", false, "enable strict type-checking semantics; any behaves as unknown and must be narrowed before acceptance where a specific type is expected (overrides lua.type_system.strict)")
	lintCmd.Flags().StringArray("profile", nil, "apply a workspace profile from the merged runtime config (repeatable, applied in order)")
	lintCmd.Flags().StringArray("set", nil, "override a merged runtime config value (format: section.path=value, repeatable)")
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
	EntryID     string
	displayCode string
	Diag        diag.Diagnostic
	Source      diag.SourceLines
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

const parseErrorCode = "P0001"

// lintConfig holds runtime configuration for a lint session.
type lintConfig struct {
	minSeverity severity
	workers     int
	imports     importResolution
	catalog     *contractCatalog
	nsFilters   []string
}

// importResolution records runtime entries separately from type manifests. A
// valid runtime target can be untyped, and a lock can name modules whose source
// has not been installed locally yet.
type importResolution struct {
	entries    map[regapi.ID]bool
	incomplete bool
}

func newImportResolution(entries []regapi.Entry, incomplete bool) importResolution {
	r := importResolution{
		entries:    make(map[regapi.ID]bool, len(entries)),
		incomplete: incomplete,
	}
	for _, entry := range entries {
		r.entries[entry.ID] = true
	}
	return r
}

func (r importResolution) definitelyMissing(id regapi.ID) bool {
	return !r.incomplete && !r.entries[id]
}

const maxLintWorkers = 8

func boundedLintWorkers(procs int) int {
	if procs < 1 {
		return 1
	}
	if procs > maxLintWorkers {
		return maxLintWorkers
	}
	return procs
}

func defaultLintWorkers() int {
	return boundedLintWorkers(runtime.GOMAXPROCS(0))
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

	runtimeCfg, err := loadRuntimeConfig(cmd, zap.NewNop())
	if err != nil {
		return err
	}
	runtimeCfg = applyTypeSystemFlags(cmd, runtimeCfg)

	ctx, loader, err := bootstrapLintContext(runtimeCfg)
	if err != nil {
		return err
	}

	if profiler {
		if err := loader.Start(ctx); err != nil {
			return fmt.Errorf("failed to start components: %w", err)
		}
		defer func() { _ = loader.Shutdown(ctx) }()
	}

	luaEntries, reportSet, resolution, catalog, err := loadLuaEntries(cmd, runtimeCfg, opts.lockFile, opts.nsFilters)
	if err != nil {
		return err
	}

	linter, lcache := createLinter(ctx, opts.enableRules, catalog)
	if opts.cacheReset {
		if err := resetLintCache(lcache); err != nil {
			return err
		}
	}
	cfg := lintConfig{
		minSeverity: opts.minSeverity,
		workers:     defaultLintWorkers(),
		imports:     resolution,
		catalog:     catalog,
		nsFilters:   opts.nsFilters,
	}

	var result *LintResult
	if console {
		result, err = runLintWithUI(cmd.Context(), luaEntries, reportSet, linter, lcache, cfg)
	} else {
		result = runLintSimple(luaEntries, reportSet, linter, lcache, cfg)
	}
	if err != nil {
		return err
	}
	appendCatalogCoverage(result, catalog, lcache.catalogStrict, opts.nsFilters, opts.minSeverity)

	result = applyFilters(result, opts.codeFilters, opts.limit)
	if pruner, ok := lcache.store.(cache.Pruner); ok && lintCacheAllowsWrite(lcache) {
		if err := pruner.Prune(); err != nil {
			return err
		}
	}
	return outputResults(result, opts)
}

// applyTypeSystemFlags writes the type-system flags given on the command line
// into the lua.type_system config section, the one place every checker reads
// its semantics from.
func applyTypeSystemFlags(cmd *cobra.Command, cfg boot.Config) boot.Config {
	if !cmd.Flags().Changed("strict") {
		return cfg
	}
	strict, _ := cmd.Flags().GetBool("strict")
	return bootconfig.Merge(cfg, boot.NewConfig(boot.WithSection("lua", map[string]any{
		"type_system.strict": strict,
	})))
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

func bootstrapLintContext(cfg boot.Config) (ctx context.Context, loader *bootpkg.Loader, err error) {
	logger, err := clilogger.CreateLogger(clilogger.Config{
		Silent:       true,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return nil, nil, NewCreateLoggerError(err)
	}

	bctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		return nil, nil, NewInitializeBootstrapContextError(err)
	}

	components := selectedComponents()
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

func loadLuaEntries(cmd *cobra.Command, runtimeCfg boot.Config, lockFile string, nsFilters []string) ([]regapi.Entry, map[regapi.ID]bool, importResolution, *contractCatalog, error) {
	logger := zap.NewNop()

	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return nil, nil, importResolution{}, nil, NewInitAppError(err)
	}
	boot.WithConfig(app.Ctx, runtimeCfg)

	lockPath, lockObj, err := loadValidatedLock(".", lockFile, runtimeCfg, logger)
	if err != nil {
		return nil, nil, importResolution{}, nil, err
	}

	allEntries, err := loadLintEntriesFromLock(app.Ctx, lockPath, lockObj, logger)
	if err != nil {
		return nil, nil, importResolution{}, nil, err
	}

	selected := filterLuaEntries(allEntries, nsFilters)
	supplemental, err := loadSupplementalAppImports(app.Ctx, lockPath, allEntries, logger)
	if err != nil {
		return nil, nil, importResolution{}, nil, err
	}
	allEntries = append(allEntries, supplemental...)
	catalog := collectContractCatalog(allEntries)
	allLua := filterLuaEntries(allEntries, nil)
	expanded, reportSet := expandLuaEntriesForCatalog(allLua, selected, catalog, nsFilters)

	return expanded, reportSet, newImportResolution(allEntries, lintLockSourcesIncomplete(lockObj)), catalog, nil
}

func expandLuaEntriesForCatalog(allLua, selected []regapi.Entry, catalog *contractCatalog, filters []string) ([]regapi.Entry, map[regapi.ID]bool) {
	selectedForExpansion := append([]regapi.Entry(nil), selected...)
	for _, entry := range allLua {
		if catalog.selectedBoundFunction(entry.ID, filters) {
			selectedForExpansion = append(selectedForExpansion, entry)
		}
	}
	expanded, _ := expandLuaEntriesByImports(allLua, selectedForExpansion)
	reportSet := make(map[regapi.ID]bool, len(selected))
	for _, entry := range selected {
		reportSet[entry.ID] = true
	}
	return expanded, reportSet
}

// A module's source tree may declare tests that import an app entry provided by
// a sibling harness lock (for example, test/wippy.lock). Include only missing
// app targets from that harness; its other entries and module selection do not
// replace the current lint workspace.
func loadSupplementalAppImports(ctx context.Context, lockPath string, loaded []regapi.Entry, logger *zap.Logger) ([]regapi.Entry, error) {
	present := make(map[regapi.ID]bool, len(loaded))
	for _, entry := range loaded {
		present[entry.ID] = true
	}
	needed := make(map[regapi.ID]bool)
	for _, entry := range filterLuaEntries(loaded, nil) {
		for _, id := range extractEntryData(entry).Imports {
			if id.NS == "app" && !present[id] {
				needed[id] = true
			}
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	children, err := os.ReadDir(filepath.Dir(lockPath))
	if err != nil {
		return nil, err
	}
	var supplemental []regapi.Entry
	for _, child := range children {
		if !child.IsDir() || strings.HasPrefix(child.Name(), ".") {
			continue
		}
		childLockPath := filepath.Join(filepath.Dir(lockPath), child.Name(), filepath.Base(lockPath))
		if _, err := os.Stat(childLockPath); err != nil {
			continue
		}
		childLock, err := lock.New(childLockPath)
		if err != nil || childLock.GetDirectories().Src == "" {
			continue
		}
		sourcePath := lock.ResolveLockPath(filepath.Dir(childLockPath), childLock.GetDirectories().Src)
		appEntries, err := entries.LoadEntriesFromModuleLoadPaths(ctx, []lock.ModuleLoadPath{{Path: sourcePath, Root: true}}, logger)
		if err != nil {
			logger.Debug("skipping supplemental app source", zap.String("lock_path", childLockPath), zap.Error(err))
			continue
		}
		for _, entry := range appEntries {
			if needed[entry.ID] {
				supplemental = append(supplemental, entry)
				delete(needed, entry.ID)
			}
		}
		if len(needed) == 0 {
			break
		}
	}
	return supplemental, nil
}

func lintLockSourcesIncomplete(lockObj *lock.Lock) bool {
	for _, path := range lockObj.GetModuleLoadPaths() {
		if path.Module == "" {
			continue
		}
		if _, err := os.Stat(path.Path); err != nil {
			return true
		}
	}
	return false
}

// loadLintEntriesFromLock includes declared workspace replacement components
// even when the lock has not selected them yet. Runtime dependency preparation
// selects these sources before loading entries; lint must see the same source
// entries to infer manifests for imports from those components.
func loadLintEntriesFromLock(ctx context.Context, lockPath string, lockObj *lock.Lock, logger *zap.Logger) ([]regapi.Entry, error) {
	paths := lockObj.GetModuleLoadPaths()
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path.Module != "" {
			seen[path.Module] = true
		}
	}

	for {
		loaded, err := entries.LoadEntriesFromModuleLoadPaths(ctx, paths, logger)
		if err != nil {
			return nil, NewLoadEntriesError(fmt.Sprintf("lock paths (%s)", lockPath), err)
		}

		added := false
		for _, dep := range extractRootDependencies(loaded, payload.GetTranscoder(ctx)) {
			module := dep.Org + "/" + dep.Module
			if seen[module] {
				continue
			}
			replacement, ok := lockObj.GetReplacement(module)
			if !ok || replacement.To == "" {
				continue
			}
			root := lock.ResolveLockPath(filepath.Dir(lockPath), replacement.To)
			paths = append(paths, lock.ModuleLoadPath{
				Path:        lock.ModuleEntryLoadPath(root),
				Module:      module,
				SourceRoot:  root,
				Root:        lockObj.IsRootModule(module),
				Replacement: true,
			})
			seen[module] = true
			added = true
		}
		if !added {
			return loaded, nil
		}
	}
}

func createLinter(ctx context.Context, enableRules bool, catalogs ...*contractCatalog) (*lint.Linter, lintCache) {
	cm := luaboot.GetCodeManager(ctx)
	var mods []*luaapi.ModuleDef
	if cm != nil {
		mods = cm.GetModuleDefs()
	}

	typeCfg := code.TypeCheckConfig{
		Enabled: true,
		Strict:  true,
	}
	if cm != nil {
		runtimeTypeCfg := cm.TypeCheckConfig()
		if runtimeTypeCfg.Enabled {
			typeCfg = runtimeTypeCfg
		}
		typeCfg.Check = runtimeTypeCfg.Check
	}
	var catalog *contractCatalog
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	var overrides map[string]*io.Manifest
	if catalog != nil && catalog.manifest != nil {
		overrides = map[string]*io.Manifest{"contract": catalog.manifest}
	}
	typeChecker := code.NewTypeCheckerWithManifests(typeCfg, mods, overrides)

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
	lcache.catalogStrict = typeCfg.Check.Strict
	var builtinManifests map[string]*io.Manifest
	lcache.builtinModules, builtinManifests = lintBuiltinInventory(mods)
	if overrides != nil {
		builtinManifests["contract"] = catalog.manifest
	}
	lcache.builtinHash = code.BuiltinManifestHash(builtinManifests)
	if catalog != nil {
		lcache.catalogHash = cache.HashStrings(catalog.fingerprint, strconv.FormatBool(typeCfg.Check.Strict))
	}

	// requireBuiltins is the set of modules a scoped require resolves without an
	// explicit import/module declaration. It mirrors the runtime ambient base
	// (engine core modules and standard libs) plus the modules every executable
	// Lua kind injects, so an undeclared require that would fail at runtime is
	// flagged at lint time. Every other registered module must be declared.
	lcache.requireBuiltins = make(map[string]struct{})
	for _, name := range engine.AmbientBaseModuleNames() {
		lcache.requireBuiltins[name] = struct{}{}
	}
	for _, name := range component.ExecutableAmbientModuleNames() {
		lcache.requireBuiltins[name] = struct{}{}
	}

	return lint.New(typeChecker, registry), lcache
}

func lintBuiltinInventory(mods []*luaapi.ModuleDef) ([]string, map[string]*io.Manifest) {
	names := make([]string, 0, len(mods))
	manifests := make(map[string]*io.Manifest)
	for _, mod := range mods {
		if mod == nil || mod.Name == "" {
			continue
		}
		names = append(names, mod.Name)
		if mod.Types == nil {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		manifests[mod.Name] = manifest
	}
	return names, manifests
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

func runLintWithUI(ctx context.Context, luaEntries []regapi.Entry, reportSet map[regapi.ID]bool, linter *lint.Linter, lcache lintCache, cfg lintConfig) (result *LintResult, err error) {
	reporter := newCLIProgressReporter(ctx, os.Stdout)
	reporter.lintTotal = len(luaEntries)
	defer reporter.Close()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("lint panic: %v", recovered)
		}
	}()

	result = lintEntries(luaEntries, reportSet, linter, lcache, cfg, reporter)
	if err := reporter.Err(); err != nil {
		return nil, err
	}
	reporter.Send(lintCompleteMsg{})
	return result, nil
}

func runLintSimple(luaEntries []regapi.Entry, reportSet map[regapi.ID]bool, linter *lint.Linter, lcache lintCache, cfg lintConfig) *LintResult {
	result := lintEntries(luaEntries, reportSet, linter, lcache, cfg, nil)
	fmt.Fprintf(os.Stderr, "\r                    \r")
	return result
}

// lintEntries is the core linting loop. If prog is non-nil, sends UI updates.
func lintEntries(luaEntries []regapi.Entry, reportSet map[regapi.ID]bool, linter *lint.Linter, lcache lintCache, cfg lintConfig, prog *cliProgressReporter) *LintResult {
	result := &LintResult{TotalEntries: len(luaEntries)}
	if cfg.imports.entries == nil {
		cfg.imports = newImportResolution(luaEntries, false)
	}
	workers := cfg.workers
	if workers < 1 {
		workers = defaultLintWorkers()
	} else {
		workers = boundedLintWorkers(workers)
	}

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
		if prog != nil && prog.Err() != nil {
			return result
		}
		if len(levelEntries) == 0 {
			continue
		}

		if len(levelEntries) == 1 {
			entry := levelEntries[0]
			er := lintOneEntry(entry, entryDataMap[entry.ID], linter, manifestMap, cfg.imports, cfg.minSeverity, lcache, fps)
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
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup

		for i, entry := range levelEntries {
			wg.Add(1)
			sem <- struct{}{}

			go func(idx int, e regapi.Entry) {
				defer wg.Done()
				defer func() { <-sem }()

				clone := linter.Clone()
				er := lintOneEntry(e, entryDataMap[e.ID], clone, manifestMap, cfg.imports, cfg.minSeverity, lcache, fps)
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
		if prog != nil && prog.Err() != nil {
			return result
		}

		for i := range results {
			mergeEntryResult(result, &results[i], manifestMap, reportSet)
		}
	}

	appendConformanceFindings(result, checkBindingConformance(cfg.catalog, manifestMap, entryDataMap, cfg.nsFilters), lcache.catalogStrict, cfg.minSeverity)
	sortLintResults(result)
	return result
}

func lintOneEntry(entry regapi.Entry, data entryData, linter *lint.Linter, manifestMap map[regapi.ID]*io.Manifest, resolution importResolution, minSev severity, lcache lintCache, fps lintFingerprints) *entryResult {
	if data.Source == "" {
		return nil
	}

	entryID := entry.ID.String()
	sourceLines := diag.ParseSource(data.Source)

	stmts, parseErr := parse.ParseString(data.Source, entryID)
	if parseErr != nil {
		return parseErrorResult(entry.ID, parseErr, sourceLines)
	}

	imports := make(map[string]*io.Manifest)
	var importDiags []diag.Diagnostic
	aliases := make([]string, 0, len(data.Imports))
	for alias := range data.Imports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		importID := data.Imports[alias]
		if importID.NS == "" {
			if manifest := linter.BuiltinManifest(importID.Name); manifest != nil {
				imports[alias] = manifest
			} else if !slices.Contains(lcache.builtinModules, importID.Name) {
				importDiags = append(importDiags, unresolvedImportDiagnostic(entryID, alias, importID))
			}
			continue
		}
		if manifest, ok := manifestMap[importID]; ok && manifest != nil {
			imports[alias] = manifest
		} else if resolution.definitelyMissing(importID) {
			importDiags = append(importDiags, unresolvedImportDiagnostic(entryID, alias, importID))
		}
	}

	var cachedManifest *io.Manifest
	var cachedDiagnostics []diag.Diagnostic
	typecheckCacheHit := false
	if tcFP := fps.typecheck[entry.ID]; tcFP != "" {
		if manifest, diags, ok := lintLoadTypecheckCache(lcache, entry.ID, tcFP); ok {
			cachedManifest = manifest
			cachedDiagnostics = diags
			typecheckCacheHit = true
		}
	}

	enableTypecheck := cachedDiagnostics == nil
	lintResult := linter.CheckParsedWithTypecheck(stmts, entryID, imports, enableTypecheck)
	linter.ClearCache()
	typeDiags := filterTypecheckDiagnostics(lintResult.Diagnostics)

	if cachedDiagnostics != nil {
		lintResult.Manifest = cachedManifest
		lintResult.Diagnostics = append(cachedDiagnostics, lintResult.Diagnostics...)
		typeDiags = cachedDiagnostics
	}

	requireDiags := lintRequireDeclarations(stmts, entryID, data, lcache.requireBuiltins)
	if len(requireDiags) > 0 {
		lintResult.Diagnostics = append(requireDiags, lintResult.Diagnostics...)
	}
	lintResult.Diagnostics = append(importDiags, lintResult.Diagnostics...)

	if lintResult.Manifest != nil && !typecheckCacheHit {
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

func unresolvedImportDiagnostic(entryID, alias string, importID regapi.ID) diag.Diagnostic {
	return diag.Diagnostic{
		Position: diag.Position{File: entryID, Line: 1, Column: 1},
		Code:     diag.ErrNoHandler,
		Severity: diag.SeverityWarning,
		Message:  fmt.Sprintf("declared import %q cannot resolve %s at runtime", alias, importID.String()),
	}
}

// parseErrorResult reports a syntax error the same way a type error is
// reported: counted, listed, and rendered with its line so it cannot pass
// unnoticed in the terminal report.
func parseErrorResult(id regapi.ID, parseErr error, sourceLines diag.SourceLines) *entryResult {
	entryID := id.String()
	message := parseErr.Error()
	position := diag.Position{File: entryID, Line: 1, Column: 1}
	var syntaxErr *parse.Error
	if errors.As(parseErr, &syntaxErr) {
		message = syntaxErr.Message
		if syntaxErr.Pos.Line == parse.EOF {
			position.Line = len(sourceLines)
			if position.Line < 1 {
				position.Line = 1
			}
		} else if syntaxErr.Pos.Line > 0 {
			position.Line = syntaxErr.Pos.Line
			if syntaxErr.Pos.Column > 0 {
				position.Column = syntaxErr.Pos.Column
			}
		}
	}
	rendered := diag.Diagnostic{Severity: diag.SeverityError, Message: message, Position: position}
	return &entryResult{
		entryID: id,
		diagnostics: []Diagnostic{{
			EntryID:  entryID,
			Code:     parseErrorCode,
			Severity: severityError.String(),
			Message:  message,
			Line:     position.Line,
			Column:   position.Column,
		}},
		rich: []RichDiagnostic{{
			EntryID:     entryID,
			Diag:        rendered,
			Source:      sourceLines,
			displayCode: parseErrorCode,
		}},
		errors: 1,
	}
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

func lintRequireDeclarations(stmts []ast.Stmt, entryID string, data entryData, builtinModules map[string]struct{}) []diag.Diagnostic {
	declared := make(map[string]struct{}, len(data.Imports))
	for alias := range data.Imports {
		if alias != "" {
			declared[alias] = struct{}{}
		}
	}

	collector := diag.NewCollector(entryID)
	checkRequire := func(call *ast.FuncCallExpr, moduleName string) {
		if _, ok := declared[moduleName]; ok {
			return
		}
		if _, ok := builtinModules[moduleName]; ok {
			return
		}
		collector.Add(call, diag.ErrNoHandler,
			"require(%q) is not declared in _index.yaml imports or modules", moduleName)
	}

	if data.Method == "" {
		walkRequireStmts(stmts, checkRequire)
		return collector.All()
	}

	// Index definitions without entering their bodies. A field is identified by its
	// name, regardless of the receiver spelling: aliases are ordinary references.
	defs := map[string][]*ast.FunctionExpr{}
	tables := map[string]bool{}
	add := func(key string, fn *ast.FunctionExpr) {
		if key != "" && fn != nil {
			defs[key] = append(defs[key], fn)
		}
	}
	var index func([]ast.Stmt)
	index = func(body []ast.Stmt) {
		walkRequireNodes(body, nil, func(stmt ast.Stmt) {
			switch v := stmt.(type) {
			case *ast.FuncDefStmt:
				if v.Name == nil {
					return
				}
				if v.Name.Method != "" {
					if id, ok := v.Name.Receiver.(*ast.IdentExpr); ok {
						tables[id.Value] = true
					}
					add("field:"+v.Name.Method, v.Func)
					return
				}
				if attr, ok := v.Name.Func.(*ast.AttrGetExpr); ok {
					if id, ok := attr.Object.(*ast.IdentExpr); ok {
						tables[id.Value] = true
					}
					add("field:"+ast.KeyName(attr.Key), v.Func)
				} else if id, ok := v.Name.Func.(*ast.IdentExpr); ok {
					add("local:"+id.Value, v.Func)
				}
			case *ast.LocalAssignStmt:
				for i, name := range v.Names {
					if i < len(v.Exprs) {
						if _, ok := v.Exprs[i].(*ast.TableExpr); ok {
							tables[name] = true
						}
						indexBinding(name, v.Exprs[i], add)
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range v.Lhs {
					if i >= len(v.Rhs) {
						break
					}
					switch name := lhs.(type) {
					case *ast.IdentExpr:
						if _, ok := v.Rhs[i].(*ast.TableExpr); ok {
							tables[name.Value] = true
						}
						indexBinding(name.Value, v.Rhs[i], add)
					case *ast.AttrGetExpr:
						add("field:"+ast.KeyName(name.Key), functionValue(v.Rhs[i]))
					}
				}
			}
		}, nil)
	}
	index(stmts)
	indexed := map[*ast.FunctionExpr]bool{}
	for {
		var pending []*ast.FunctionExpr
		for _, functions := range defs {
			for _, fn := range functions {
				if !indexed[fn] {
					indexed[fn] = true
					pending = append(pending, fn)
				}
			}
		}
		if len(pending) == 0 {
			break
		}
		for _, fn := range pending {
			index(fn.Stmts)
		}
	}

	roots := defs["field:"+data.Method]
	roots = append(append([]*ast.FunctionExpr(nil), roots...), defs["local:"+data.Method]...)
	if len(roots) == 0 {
		walkRequireStmts(stmts, checkRequire)
		return collector.All()
	}

	seen := map[*ast.FunctionExpr]bool{}
	queue := append([]*ast.FunctionExpr(nil), roots...)
	enqueueFields := func() {
		for name, fns := range defs {
			if strings.HasPrefix(name, "field:") {
				queue = append(queue, fns...)
			}
		}
	}
	// The chunk executes for every entry. Definition targets are not references;
	// only the code around them can enqueue another function.
	scan := func(body []ast.Stmt, top bool) {
		walkRequireNodes(body, checkRequire, func(stmt ast.Stmt) {
			if ret, ok := stmt.(*ast.ReturnStmt); ok && !top {
				for _, value := range ret.Exprs {
					if id, ok := value.(*ast.IdentExpr); ok && tables[id.Value] {
						enqueueFields()
					}
				}
			}
			if assign, ok := stmt.(*ast.LocalAssignStmt); ok {
				for i, name := range assign.Names {
					if i < len(assign.Exprs) {
						if source, ok := assign.Exprs[i].(*ast.IdentExpr); ok && tables[source.Value] {
							tables[name] = true
						}
					}
				}
			}
		}, func(expr ast.Expr) {
			var key string
			switch e := expr.(type) {
			case *ast.IdentExpr:
				key = "local:" + e.Value
			case *ast.AttrGetExpr:
				if _, constant := e.Key.(*ast.StringExpr); !constant {
					key = "field:*"
				} else if name := ast.KeyName(e.Key); name != "" {
					key = "field:" + name
				} else {
					key = "field:*"
				}
			case *ast.FuncCallExpr:
				if e.Method != "" {
					key = "field:" + e.Method
				}
				// A function table passed to unknown code can expose any member.
				for _, arg := range e.Args {
					if id, ok := arg.(*ast.IdentExpr); ok && tables[id.Value] {
						key = "field:*"
					}
				}
			case *ast.FunctionExpr:
				if !top {
					queue = append(queue, e)
				}
			}
			if key == "field:*" {
				enqueueFields()
			} else {
				queue = append(queue, defs[key]...)
			}
		})
	}
	scan(stmts, true)
	for len(queue) != 0 {
		fn := queue[0]
		queue = queue[1:]
		if seen[fn] || fn == nil {
			continue
		}
		seen[fn] = true
		scan(fn.Stmts, false)
	}
	return collector.All()
}

func functionValue(expr ast.Expr) *ast.FunctionExpr {
	if fn, ok := expr.(*ast.FunctionExpr); ok {
		return fn
	}
	switch expr.(type) {
	case nil, *ast.NumberExpr, *ast.StringExpr, *ast.TrueExpr, *ast.FalseExpr, *ast.NilExpr, *ast.TableExpr:
		return nil
	}
	// An expression can produce a function (for example, M.run = factory()).
	// Keep its evaluation as a root without guessing the returned function.
	return &ast.FunctionExpr{Stmts: []ast.Stmt{&ast.ReturnStmt{Exprs: []ast.Expr{expr}}}}
}

func indexBinding(name string, expr ast.Expr, add func(string, *ast.FunctionExpr)) {
	add("local:"+name, functionValue(expr))
	if table, ok := expr.(*ast.TableExpr); ok {
		for _, field := range table.Fields {
			add("field:"+ast.KeyName(field.Key), functionValue(field.Value))
		}
	}
}

func walkRequireStmts(stmts []ast.Stmt, visit func(*ast.FuncCallExpr, string)) {
	walkRequireNodes(stmts, visit, nil, nil)
}

func walkRequireNodes(stmts []ast.Stmt, visit func(*ast.FuncCallExpr, string), onStmt func(ast.Stmt), onExpr func(ast.Expr)) {
	for _, stmt := range stmts {
		walkRequireStmt(stmt, visit, onStmt, onExpr)
	}
}

func walkRequireStmt(stmt ast.Stmt, visit func(*ast.FuncCallExpr, string), onStmt func(ast.Stmt), onExpr func(ast.Expr)) {
	if stmt == nil {
		return
	}

	if onStmt != nil {
		onStmt(stmt)
	}
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if onExpr == nil {
			for _, expr := range s.Lhs {
				walkRequireExpr(expr, visit, onStmt, onExpr)
			}
		}
		for _, expr := range s.Rhs {
			walkRequireExpr(expr, visit, onStmt, onExpr)
		}
	case *ast.LocalAssignStmt:
		for _, expr := range s.Exprs {
			walkRequireExpr(expr, visit, onStmt, onExpr)
		}
	case *ast.FuncCallStmt:
		walkRequireExpr(s.Expr, visit, onStmt, onExpr)
	case *ast.DoBlockStmt:
		walkRequireNodes(s.Stmts, visit, onStmt, onExpr)
	case *ast.WhileStmt:
		walkRequireExpr(s.Condition, visit, onStmt, onExpr)
		walkRequireNodes(s.Stmts, visit, onStmt, onExpr)
	case *ast.RepeatStmt:
		walkRequireNodes(s.Stmts, visit, onStmt, onExpr)
		walkRequireExpr(s.Condition, visit, onStmt, onExpr)
	case *ast.IfStmt:
		walkRequireExpr(s.Condition, visit, onStmt, onExpr)
		walkRequireNodes(s.Then, visit, onStmt, onExpr)
		walkRequireNodes(s.Else, visit, onStmt, onExpr)
	case *ast.NumberForStmt:
		walkRequireExpr(s.Init, visit, onStmt, onExpr)
		walkRequireExpr(s.Limit, visit, onStmt, onExpr)
		walkRequireExpr(s.Step, visit, onStmt, onExpr)
		walkRequireNodes(s.Stmts, visit, onStmt, onExpr)
	case *ast.GenericForStmt:
		for _, expr := range s.Exprs {
			walkRequireExpr(expr, visit, onStmt, onExpr)
		}
		walkRequireNodes(s.Stmts, visit, onStmt, onExpr)
	case *ast.FuncDefStmt:
		if s.Func != nil && onStmt == nil && onExpr == nil {
			walkRequireNodes(s.Func.Stmts, visit, onStmt, onExpr)
		}
	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			walkRequireExpr(expr, visit, onStmt, onExpr)
		}
	}
}

func walkRequireExpr(expr ast.Expr, visit func(*ast.FuncCallExpr, string), onStmt func(ast.Stmt), onExpr func(ast.Expr)) {
	if expr == nil {
		return
	}

	if onExpr != nil {
		onExpr(expr)
	}
	switch e := expr.(type) {
	case *ast.FuncCallExpr:
		if ident, ok := e.Func.(*ast.IdentExpr); ok && ident.Value == "require" && e.Receiver == nil && e.Method == "" && len(e.Args) > 0 {
			if mod, ok := e.Args[0].(*ast.StringExpr); ok && mod.Value != "" {
				if visit != nil {
					visit(e, mod.Value)
				}
			}
		}
		walkRequireExpr(e.Func, visit, onStmt, onExpr)
		walkRequireExpr(e.Receiver, visit, onStmt, onExpr)
		for _, arg := range e.Args {
			walkRequireExpr(arg, visit, onStmt, onExpr)
		}
	case *ast.AttrGetExpr:
		walkRequireExpr(e.Object, visit, onStmt, onExpr)
		walkRequireExpr(e.Key, visit, onStmt, onExpr)
	case *ast.TableExpr:
		for _, field := range e.Fields {
			walkRequireExpr(field.Key, visit, onStmt, onExpr)
			walkRequireExpr(field.Value, visit, onStmt, onExpr)
		}
	case *ast.FunctionExpr:
		if onStmt == nil && onExpr == nil {
			walkRequireNodes(e.Stmts, visit, onStmt, onExpr)
		}
	case *ast.LogicalOpExpr:
		walkRequireExpr(e.Lhs, visit, onStmt, onExpr)
		walkRequireExpr(e.Rhs, visit, onStmt, onExpr)
	case *ast.RelationalOpExpr:
		walkRequireExpr(e.Lhs, visit, onStmt, onExpr)
		walkRequireExpr(e.Rhs, visit, onStmt, onExpr)
	case *ast.ArithmeticOpExpr:
		walkRequireExpr(e.Lhs, visit, onStmt, onExpr)
		walkRequireExpr(e.Rhs, visit, onStmt, onExpr)
	case *ast.StringConcatOpExpr:
		walkRequireExpr(e.Lhs, visit, onStmt, onExpr)
		walkRequireExpr(e.Rhs, visit, onStmt, onExpr)
	case *ast.UnaryMinusOpExpr:
		walkRequireExpr(e.Expr, visit, onStmt, onExpr)
	case *ast.UnaryNotOpExpr:
		walkRequireExpr(e.Expr, visit, onStmt, onExpr)
	case *ast.UnaryLenOpExpr:
		walkRequireExpr(e.Expr, visit, onStmt, onExpr)
	case *ast.UnaryBNotOpExpr:
		walkRequireExpr(e.Expr, visit, onStmt, onExpr)
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
		data := entryDataFromMap(m)
		data.Method = code.EffectiveMethod(entry.Kind, data.Method)
		return data
	}
	if m, ok := entry.Data.Data().(map[string]interface{}); ok {
		data := entryDataFromMap(m)
		data.Method = code.EffectiveMethod(entry.Kind, data.Method)
		return data
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

	return entryData{Source: cfg.Source, Imports: imports, Method: code.EffectiveMethod(entry.Kind, cfg.Method)}
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
	if code := richDiagnosticCode(rd); code != rd.Diag.Code.Name() {
		rendered = strings.Replace(rendered, rd.Diag.Code.Name(), code, 1)
	}
	return rendered
}

func richDiagnosticCode(rd RichDiagnostic) string {
	if rd.displayCode != "" {
		return rd.displayCode
	}
	return formatDiagCode(rd.Diag.Code)
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
		aCode, bCode := richDiagnosticCode(a), richDiagnosticCode(b)
		if aCode != bCode {
			return aCode < bCode
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
		if codeSet[strings.ToUpper(richDiagnosticCode(rd))] {
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

type lintProgressMsg struct {
	entry      string
	percent    float64
	checked    int
	errors     int
	warnings   int
	entryIssue int
}

type lintCompleteMsg struct{}
