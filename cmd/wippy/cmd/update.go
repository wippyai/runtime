// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/semver"
	"github.com/wippyai/runtime/api/version"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"go.uber.org/zap"
)

var updateCmd = &cobra.Command{
	Use:   "update [module...]",
	Short: "Update dependencies and regenerate lock file",
	Long: `Update dependencies and regenerate wippy.lock file

Without arguments, scans source directory and re-resolves the entire dependency graph,
updating all modules to their latest compatible versions.

With module arguments, updates only the specified modules to their highest version
compatible with other locked dependencies. New transitive dependencies are auto-added.
If updating would require changing other locked modules, shows impact and asks for confirmation.

Examples:
  wippy update                    # Re-resolve all dependencies from source
  wippy update acme/http          # Update only acme/http
  wippy update acme/http demo/sql # Update multiple specific modules`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	updateCmd.Flags().StringP("src-dir", "d", "./src", "source directory path")
	updateCmd.Flags().String("modules-dir", ".wippy", "modules directory path")
	updateCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
	updateCmd.Flags().StringArray("profile", nil, "apply a workspace profile from the merged runtime config (repeatable, applied in order)")
	updateCmd.Flags().StringArray("set", nil, "override a merged runtime config value (format: section.path=value, repeatable)")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	logger := app.Logger.Named("update")
	runtimeCfg, err := loadRuntimeConfig(cmd, logger)
	if err != nil {
		return err
	}

	lockFilePath, _ := cmd.Flags().GetString("lock-file")
	registryURL, _ := cmd.Flags().GetString("registry")

	// Get flag values and check if explicitly set
	srcDir, _ := cmd.Flags().GetString("src-dir")
	modulesDir, _ := cmd.Flags().GetString("modules-dir")
	srcDirChanged := cmd.Flags().Changed("src-dir")
	modulesDirChanged := cmd.Flags().Changed("modules-dir")

	// Load existing lock file to get current directories
	var existingDirs *lock.Directories
	if stat, err := os.Stat(lockFilePath); err == nil && !stat.IsDir() {
		if existingLock, err := lock.New(lockFilePath); err == nil {
			dirs := existingLock.GetDirectories()
			existingDirs = &dirs
		}
	}

	// Use existing directories unless flags explicitly override
	if existingDirs != nil {
		if !srcDirChanged && existingDirs.Src != "" {
			srcDir = existingDirs.Src
		}
		if !modulesDirChanged && existingDirs.Modules != "" {
			modulesDir = existingDirs.Modules
		}
	}

	// Get auth credentials
	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	cred, _ := store.Get(registryURL)
	var token string
	if cred != nil {
		token = cred.Token
	}

	// Create hub client
	hubClient, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return NewCreateHubClientError(fmt.Errorf("registry %s: %w", registryURL, err))
	}

	// Targeted update if modules specified
	if len(args) > 0 {
		return runTargetedUpdate(cmd, lockFilePath, srcDir, modulesDir, args, app, hubClient, runtimeCfg)
	}

	// Full update otherwise
	logger.Info("re-resolving all dependencies from source")

	// Load old lock file for comparison
	var oldLockObj *lock.Lock
	if stat, err := os.Stat(lockFilePath); err == nil && !stat.IsDir() {
		oldLockObj, err = newConfiguredLock(lockFilePath, runtimeCfg, logger)
		if err != nil {
			return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockFilePath, err))
		}
		if err := lock.Validate(oldLockObj); err != nil {
			return NewInvalidExistingLockFileError(fmt.Errorf("lock file %s: %w", lockFilePath, err))
		}
	}

	// Scan app source plus local replacement sources for dependency entries.
	// Replacement modules are not resolved from the hub, but their transitive
	// ns.dependency entries are part of the active application graph.
	logger.Info("scanning dependency sources", zap.String("src_dir", srcDir))

	entries, err := loadDependencyScanEntries(app.Ctx, app.Loader, srcDir, oldLockObj, logger)
	if err != nil {
		return NewLoadEntriesFromSourceError(err)
	}

	replacedModules := effectiveReplacementModules(oldLockObj)
	dependencyGraph, err := resolveDependencyGraph(entries, app.Transcoder, replacedModules)
	if err != nil {
		return NewLoadEntriesFromSourceError(err)
	}
	rootDeps := dependencyGraph.dependencies
	logger.Info("found root dependencies", zap.Int("count", len(rootDeps)))

	resolvedModules := make([]hub.ResolvedModule, 0)
	if len(rootDeps) == 0 {
		logger.Info("no root dependencies found in source, pruning lock modules")
	} else {
		// Convert to hub dependency specs, skipping replaced modules
		hubDeps := make([]hub.DependencySpec, 0, len(rootDeps))
		for _, dep := range rootDeps {
			hubDeps = append(hubDeps, hub.DependencySpec{
				Org:             dep.Org,
				Name:            dep.Module,
				Constraint:      dep.Constraint,
				BuildOnly:       dep.BuildOnly,
				BuildDependency: dep.BuildDependency,
			})
		}

		logger.Info("resolving dependency graph")
		result, err := hub.Resolve(app.Ctx, hubClient, hubDeps, nil)
		if err != nil {
			return NewBuildDependencyGraphError(err)
		}

		if len(result.Errors) > 0 {
			logger.Error("dependency resolution errors", zap.Int("count", len(result.Errors)))
			for _, resErr := range result.Errors {
				logger.Error("error", zap.String("module", resErr.Org+"/"+resErr.Name), zap.String("reason", resErr.Message))
			}
			return newResolutionConflictsError("dependency conflicts detected", result.Errors)
		}

		logger.Info("dependency graph resolved", zap.Int("total_modules", len(result.Modules)))
		resolvedModules = result.Modules
	}

	// Convert resolved modules to lock file
	newLockObj, err := convertResolvedToLock(lockFilePath, resolvedModules, modulesDir, srcDir, effectiveReplacementOption(oldLockObj))
	if err != nil {
		return NewLoadLockFileError(err)
	}

	// Preserve all replacements from old lock file
	if oldLockObj != nil {
		preserveReplacements(newLockObj, oldLockObj.GetTrackedReplacements())
	}
	preserveBuildReplacementModules(newLockObj, oldLockObj, dependencyGraph.replacements)
	if err := lock.Validate(newLockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("generated lock file %s: %w", newLockObj.Path(), err))
	}

	// Save lock file
	if err := newLockObj.Write(); err != nil {
		return NewWriteLockFileError(fmt.Errorf("lock file %s: %w", newLockObj.Path(), err))
	}

	logger.Info("lock file updated")

	// Compare old and new
	var changes *lock.Changes
	if oldLockObj != nil {
		changes = lock.Diff(oldLockObj, newLockObj)
		logChanges(logger, changes)
		pruneStaleVendorArtifacts(newLockObj, changes, logger)
	}

	if len(resolvedModules) > 0 {
		// Run install to download modules
		logger.Info("running install to download modules")
		if err := runInstall(cmd, []string{}); err != nil {
			return NewInstallFailedError(err)
		}
	} else if len(replacedModules) == 0 {
		logger.Info("no modules to install after update")
	}

	logger.Info("update completed successfully")
	return nil
}

type dependencyRequest struct {
	Org             string
	Module          string
	Constraint      string
	BuildOnly       bool
	BuildDependency bool
}

func containsBuildDependency(entries []regapi.Entry) bool {
	for _, entry := range entries {
		if entry.Kind == regapi.NamespaceBuildDependency {
			return true
		}
	}
	return false
}

func validateBuildDependencyRuntime(cfg *depconfig.ModuleConfig, entries []regapi.Entry, current string) error {
	if !containsBuildDependency(entries) {
		return nil
	}
	if cfg == nil || strings.TrimSpace(cfg.RequiresWippy) == "" {
		return fmt.Errorf("ns.build_dependency requires requires_wippy in wippy.yaml")
	}
	return cfg.ValidateRuntimeVersion(current)
}

type dependencyDeclaration struct {
	dependencyRequest
	owner string
}

type resolvedDependencyGraph struct {
	dependencies []dependencyRequest
	replacements []dependencyRequest
}

func extractRootDependencies(entries []regapi.Entry, dtt payload.Transcoder, replacedModules map[string]bool) ([]dependencyRequest, error) {
	graph, err := resolveDependencyGraph(entries, dtt, replacedModules)
	return graph.dependencies, err
}

func resolveDependencyGraph(entries []regapi.Entry, dtt payload.Transcoder, replacedModules map[string]bool) (resolvedDependencyGraph, error) {
	declarations, err := decodeDependencyDeclarations(entries, dtt)
	if err != nil {
		return resolvedDependencyGraph{}, err
	}

	byOwner := make(map[string][]dependencyRequest)
	for _, declaration := range declarations {
		byOwner[declaration.owner] = append(byOwner[declaration.owner], declaration.dependencyRequest)
	}

	type reachability struct {
		module  string
		runtime bool
		build   bool
	}
	queue := []reachability{{runtime: true}}
	replacementRoles := make(map[string]reachability)
	replacementIndexes := make(map[string]int)
	replacements := make([]dependencyRequest, 0, len(replacedModules))
	dependencies := make([]dependencyRequest, 0, len(declarations))
	seen := make(map[string]int)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependency := range byOwner[current.module] {
			edgeBuild := dependency.BuildDependency
			dependency.BuildOnly = !(current.runtime && !edgeBuild)
			dependency.BuildDependency = current.build || edgeBuild
			component := dependency.Org + "/" + dependency.Module
			if replacedModules[component] {
				previous, visited := replacementRoles[component]
				combined := reachability{
					module:  component,
					runtime: previous.runtime || !dependency.BuildOnly,
					build:   previous.build || dependency.BuildDependency,
				}
				if !visited {
					replacementIndexes[component] = len(replacements)
					replacements = append(replacements, dependency)
				} else {
					index := replacementIndexes[component]
					replacements[index].BuildOnly = !combined.runtime
					replacements[index].BuildDependency = combined.build
				}
				if !visited || combined.runtime != previous.runtime || combined.build != previous.build {
					replacementRoles[component] = combined
					queue = append(queue, combined)
				}
				continue
			}

			key := component + "@" + dependency.Constraint
			if index, ok := seen[key]; ok {
				dependencies[index].BuildOnly = dependencies[index].BuildOnly && dependency.BuildOnly
				dependencies[index].BuildDependency = dependencies[index].BuildDependency || dependency.BuildDependency
				continue
			}
			seen[key] = len(dependencies)
			dependencies = append(dependencies, dependency)
		}
	}

	return resolvedDependencyGraph{dependencies: dependencies, replacements: replacements}, nil
}

func decodeDependencyDeclarations(entries []regapi.Entry, dtt payload.Transcoder) ([]dependencyDeclaration, error) {
	declarations := make([]dependencyDeclaration, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != regapi.NamespaceDependency && entry.Kind != regapi.NamespaceBuildDependency {
			continue
		}

		buildOnly := entry.Kind == regapi.NamespaceBuildDependency
		var data struct {
			Component  string `json:"component"`
			Version    string `json:"version"`
			Parameters []any  `json:"parameters"`
		}
		if err := dtt.Unmarshal(entry.Data, &data); err != nil {
			if buildOnly {
				return nil, fmt.Errorf("decode dependency %s: %w", entry.ID.String(), err)
			}
			continue
		}

		parts := strings.SplitN(data.Component, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			if buildOnly {
				return nil, fmt.Errorf("build dependency %s has invalid component %s", entry.ID.String(), data.Component)
			}
			continue
		}
		if buildOnly && len(data.Parameters) > 0 {
			return nil, fmt.Errorf("build dependency %s cannot declare parameters", entry.ID.String())
		}
		if buildOnly {
			if _, err := semver.ParseVersion(data.Version); err != nil {
				return nil, fmt.Errorf("build dependency %s must use an exact semver version: %s", entry.ID.String(), data.Version)
			}
		}

		owner := ""
		if entry.Meta != nil {
			owner = entry.Meta.GetString("module", "")
		}
		declarations = append(declarations, dependencyDeclaration{
			dependencyRequest: dependencyRequest{
				Org:             parts[0],
				Module:          parts[1],
				Constraint:      data.Version,
				BuildOnly:       buildOnly,
				BuildDependency: buildOnly,
			},
			owner: owner,
		})
	}
	return declarations, nil
}

func convertResolvedToLock(lockFilePath string, modules []hub.ResolvedModule, modulesDir, srcDir string, options ...lock.Option) (*lock.Lock, error) {
	lockObj, err := lock.New(lockFilePath, options...)
	if err != nil {
		return nil, fmt.Errorf("lock file %s: %w", lockFilePath, err)
	}

	lockObj.SetDirectories(lock.Directories{
		Modules: modulesDir,
		Src:     srcDir,
	})

	lockedModules := make([]lock.Module, 0, len(modules))
	for _, m := range modules {
		lockedModules = append(lockedModules, lock.Module{
			Name:            fmt.Sprintf("%s/%s", m.Org, m.Name),
			Version:         m.Version,
			Hash:            m.Digest,
			BuildOnly:       m.BuildOnly,
			BuildDependency: m.BuildDependency,
		})
	}
	lockObj.ReplaceModules(lockedModules)

	return lockObj, nil
}

func runTargetedUpdate(cmd *cobra.Command, lockFilePath, srcDir, modulesDir string, targetModules []string, app *appinit.Context, hubClient *hub.Client, runtimeCfg boot.Config) error {
	logger := app.Logger.Named("update")
	logger.Info("updating specific modules", zap.Strings("modules", targetModules))

	// Load current lock file
	lockObj, err := newConfiguredLock(lockFilePath, runtimeCfg, logger)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockFilePath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	oldLockObj := lockObj

	replacedModules := effectiveReplacementModules(lockObj)

	effectiveTargets := make([]string, 0, len(targetModules))
	for _, moduleName := range targetModules {
		if repl, ok := lockObj.GetReplacement(moduleName); ok {
			logger.Info("requested module is replaced by local source; skipping hub update",
				zap.String("module", moduleName),
				zap.String("replacement", repl.To))
			continue
		}
		effectiveTargets = append(effectiveTargets, moduleName)
	}
	if len(effectiveTargets) == 0 {
		logger.Info("all requested modules are local replacements; nothing to update")
		return nil
	}

	// Scan app source plus local replacement sources to get constraints.
	entries, err := loadDependencyScanEntries(app.Ctx, app.Loader, srcDir, lockObj, logger)
	if err != nil {
		return NewLoadEntriesFromSourceError(err)
	}

	dependencyGraph, err := resolveDependencyGraph(entries, app.Transcoder, replacedModules)
	if err != nil {
		return NewLoadEntriesFromSourceError(err)
	}
	rootDeps := dependencyGraph.dependencies
	targetSet := make(map[string]bool, len(effectiveTargets))
	for _, name := range effectiveTargets {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return NewParseModuleNameError(name, fmt.Errorf("invalid format, expected org/module"))
		}
		targetSet[name] = true
	}

	hubDeps := make([]hub.DependencySpec, 0, len(rootDeps))
	for _, dependency := range rootDeps {
		hubDeps = append(hubDeps, hub.DependencySpec{
			Org:             dependency.Org,
			Name:            dependency.Module,
			Constraint:      dependency.Constraint,
			BuildOnly:       dependency.BuildOnly,
			BuildDependency: dependency.BuildDependency,
		})
	}

	logger.Info("resolving with frozen dependencies")
	result, err := hub.Resolve(app.Ctx, hubClient, hubDeps, targetedResolveOptions(lockObj, targetSet, replacedModules))
	if err != nil {
		return NewBuildDependencyGraphError(err)
	}

	if len(result.Errors) > 0 {
		logger.Error("resolution errors", zap.Int("count", len(result.Errors)))
		for _, resErr := range result.Errors {
			logger.Error("error", zap.String("module", resErr.Org+"/"+resErr.Name), zap.String("reason", resErr.Message))
		}
		return newResolutionConflictsError("update conflicts detected", result.Errors)
	}

	// Build new lock file
	newLockObj, err := convertResolvedToLock(lockFilePath, result.Modules, modulesDir, srcDir, effectiveReplacementOption(oldLockObj))
	if err != nil {
		return NewLoadLockFileError(err)
	}

	// Preserve all replacements from current lock file
	preserveReplacements(newLockObj, lockObj.GetTrackedReplacements())
	preserveBuildReplacementModules(newLockObj, oldLockObj, dependencyGraph.replacements)
	if err := lock.Validate(newLockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("generated lock file %s: %w", newLockObj.Path(), err))
	}

	// Detect changes
	changes := lock.Diff(oldLockObj, newLockObj)

	// Check if any non-target modules would be updated
	var nonTargetUpdates []lock.ModuleChange
	for _, change := range changes.Updated {
		if !targetSet[change.Name] {
			nonTargetUpdates = append(nonTargetUpdates, change)
		}
	}

	// Show impact if non-target modules would be updated
	if len(nonTargetUpdates) > 0 || len(changes.Installed) > 0 {
		logger.Warn("updating target modules would affect other dependencies")

		if len(changes.Installed) > 0 {
			logger.Info("new dependencies to be added", zap.Int("count", len(changes.Installed)))
			for _, mod := range changes.Installed {
				logger.Info("+ new", zap.String("module", mod.Name), zap.String("version", mod.Version))
			}
		}

		if len(nonTargetUpdates) > 0 {
			logger.Warn("other modules would also be updated", zap.Int("count", len(nonTargetUpdates)))
			for _, change := range nonTargetUpdates {
				logger.Warn("~ required update",
					zap.String("module", change.Name),
					zap.String("from", change.OldVersion),
					zap.String("to", change.NewVersion))
			}

			// Prompt user for confirmation
			fmt.Printf("\nProceed with update? [Y/n] ")
			var response string
			if _, err := fmt.Scanln(&response); err != nil || response == "" {
				response = "Y"
			}
			if response != "" && response != "Y" && response != "y" {
				logger.Info("update canceled by user")
				return nil
			}
		}
	}

	// Save lock file
	if err := newLockObj.Write(); err != nil {
		return NewWriteLockFileError(fmt.Errorf("lock file %s: %w", newLockObj.Path(), err))
	}

	logger.Info("lock file updated")
	logChanges(logger, changes)
	pruneStaleVendorArtifacts(newLockObj, changes, logger)

	// Run install
	logger.Info("running install to download modules")
	if err := runInstall(cmd, []string{}); err != nil {
		return NewInstallFailedError(err)
	}

	logger.Info("update completed successfully")
	return nil
}

func loadDependencyScanEntries(ctx context.Context, ldr boot.Loader, srcDir string, lockObj *lock.Lock, logger *zap.Logger) ([]regapi.Entry, error) {
	if ldr == nil {
		return nil, fmt.Errorf("loader not available")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	moduleRoot, _ := os.Getwd()
	if lockObj != nil {
		moduleRoot = filepath.Dir(lockObj.Path())
	}
	paths := []struct {
		label  string
		path   string
		root   string
		module string
	}{
		{label: "source", path: srcDir, root: moduleRoot},
	}

	if lockObj != nil {
		replacements := effectiveReplacementModules(lockObj)
		for _, mp := range lockObj.GetArtifactModuleLoadPaths() {
			if mp.Module == "" || !replacements[mp.Module] {
				continue
			}
			replacementRoot := mp.SourceRoot
			if replacementRoot == "" {
				replacementRoot = mp.Path
			}
			paths = append(paths, struct {
				label  string
				path   string
				root   string
				module string
			}{
				label:  "replacement " + mp.Module,
				path:   mp.Path,
				root:   replacementRoot,
				module: mp.Module,
			})
		}
	}

	seen := make(map[string]bool, len(paths))
	entries := make([]regapi.Entry, 0)
	for _, scanPath := range paths {
		absPath, err := filepath.Abs(scanPath.path)
		if err != nil {
			return nil, fmt.Errorf("%s path %s: %w", scanPath.label, scanPath.path, err)
		}
		if seen[absPath] {
			continue
		}
		seen[absPath] = true

		logger.Info("scanning dependency source",
			zap.String("kind", scanPath.label),
			zap.String("path", absPath))

		cfg, configErr := depconfig.Load(scanPath.root)
		sourceFS := depconfig.NewSourceFS(os.DirFS(absPath), cfg, scanPath.root, absPath)
		loaded, err := ldr.LoadFS(ctx, sourceFS)
		if err != nil {
			return nil, fmt.Errorf("%s path %s: %w", scanPath.label, absPath, err)
		}
		if containsBuildDependency(loaded) {
			if configErr != nil {
				return nil, fmt.Errorf("%s config: %w", scanPath.label, configErr)
			}
			if err := validateBuildDependencyRuntime(cfg, loaded, version.Short()); err != nil {
				return nil, fmt.Errorf("%s: %w", scanPath.label, err)
			}
		}
		if scanPath.module != "" {
			for i := range loaded {
				meta := attrs.NewBag()
				if loaded[i].Meta != nil {
					meta = attrs.NewBagFrom(loaded[i].Meta)
				}
				meta.Set("module", scanPath.module)
				loaded[i].Meta = meta
			}
		}
		entries = append(entries, loaded...)
	}

	return entries, nil
}

func effectiveReplacementOption(lockObj *lock.Lock) lock.Option {
	if lockObj == nil {
		return lock.WithWorkspaceReplacements(nil)
	}
	return lock.WithWorkspaceReplacements(lockObj.GetReplacements())
}

func effectiveReplacementModules(lockObj *lock.Lock) map[string]bool {
	modules := make(map[string]bool)
	if lockObj == nil {
		return modules
	}
	for _, replacement := range lockObj.GetReplacements() {
		modules[replacement.From] = true
	}
	return modules
}

func targetedResolveOptions(lockObj *lock.Lock, targets, replacements map[string]bool) *hub.ResolveOptions {
	versions := make(map[string]string)
	digests := make(map[string]string)
	for _, module := range lockObj.GetModules() {
		if targets[module.Name] || replacements[module.Name] {
			continue
		}
		versions[module.Name] = module.Version
		if module.Hash != "" {
			digests[module.Name+"@"+module.Version] = module.Hash
		}
	}
	return &hub.ResolveOptions{LockedVersions: versions, LockedDigests: digests}
}

func logChanges(logger *zap.Logger, changes *lock.Changes) {
	if len(changes.Installed)+len(changes.Updated)+len(changes.Removed) > 0 {
		logger.Info("changes detected",
			zap.Int("installed", len(changes.Installed)),
			zap.Int("updated", len(changes.Updated)),
			zap.Int("removed", len(changes.Removed)))

		for _, mod := range changes.Installed {
			logger.Info("+ installing", zap.String("module", mod.Name), zap.String("version", mod.Version))
		}
		for _, mod := range changes.Updated {
			logger.Info("~ updating", zap.String("module", mod.Name),
				zap.String("old", mod.OldVersion), zap.String("new", mod.NewVersion),
				zap.Bool("old_build_only", mod.OldBuildOnly), zap.Bool("new_build_only", mod.NewBuildOnly),
				zap.Bool("old_build_dependency", mod.OldBuildDependency), zap.Bool("new_build_dependency", mod.NewBuildDependency))
		}
		for _, mod := range changes.Removed {
			logger.Info("- removing", zap.String("module", mod.Name), zap.String("version", mod.Version))
		}
	} else {
		logger.Info("no changes detected")
	}
}

func preserveReplacements(lockObj *lock.Lock, replacements []lock.Replacement) {
	if lockObj == nil || len(replacements) == 0 {
		return
	}

	for _, repl := range replacements {
		lockObj.SetReplacement(repl)
	}
}

func preserveBuildReplacementModules(lockObj, oldLockObj *lock.Lock, replacements []dependencyRequest) {
	oldModules := make(map[string]lock.Module)
	if oldLockObj != nil {
		for _, module := range oldLockObj.GetModules() {
			oldModules[module.Name] = module
		}
	}
	modules := lockObj.GetModules()
	indexes := make(map[string]int, len(modules)+len(replacements))
	for index, module := range modules {
		indexes[module.Name] = index
	}

	for _, replacement := range replacements {
		if !replacement.BuildDependency {
			continue
		}
		name := replacement.Org + "/" + replacement.Module
		module := lock.Module{
			Name: name, Version: replacement.Constraint,
			BuildOnly: replacement.BuildOnly, BuildDependency: true,
		}
		if oldModule, ok := oldModules[name]; ok {
			module.Version = oldModule.Version
			module.Root = oldModule.Root
		}
		if index, exists := indexes[name]; exists {
			modules[index] = module
			continue
		}
		indexes[name] = len(modules)
		modules = append(modules, module)
	}
	lockObj.ReplaceModules(modules)
}

func pruneStaleVendorArtifacts(lockObj *lock.Lock, changes *lock.Changes, logger *zap.Logger) {
	if lockObj == nil || changes == nil {
		return
	}
	if len(changes.Removed) == 0 && len(changes.Updated) == 0 {
		return
	}

	lockDir := filepath.Dir(lockObj.Path())
	vendorDir := lock.ResolveLockPath(lockDir, lockObj.GetVendorPath())

	for _, removed := range changes.Removed {
		pruneModuleArtifacts(vendorDir, removed.Name, removed.Version, true, logger)
	}
	for _, updated := range changes.Updated {
		if updated.OldVersion == updated.NewVersion && updated.OldHash == updated.NewHash {
			continue
		}
		pruneModuleArtifacts(vendorDir, updated.Name, updated.OldVersion, true, logger)
	}
}

func pruneModuleArtifacts(vendorDir, moduleName, moduleVersion string, removeCurrentDir bool, logger *zap.Logger) {
	name, err := graph.ParseName(moduleName)
	if err != nil {
		if logger != nil {
			logger.Debug("skipping stale module cleanup for invalid module name",
				zap.String("module", moduleName),
				zap.Error(err))
		}
		return
	}

	paths := []string{
		filepath.Join(vendorDir, lock.WappPath(name, moduleVersion)),
		filepath.Join(vendorDir, lock.LegacyModulePath(name, moduleVersion)),
	}
	if removeCurrentDir {
		paths = append(paths, filepath.Join(vendorDir, lock.ModulePath(name)))
	}

	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			if logger != nil {
				logger.Warn("failed to prune stale module artifact",
					zap.String("path", path),
					zap.Error(err))
			}
		}
	}
}

func newResolutionConflictsError(prefix string, errs []hub.ResolutionError) apierror.Error {
	if len(errs) == 0 {
		return apierror.New(apierror.Invalid, prefix+" (0)").WithRetryable(apierror.False)
	}
	details := make([]string, 0, len(errs))
	for _, resErr := range errs {
		details = append(details, formatResolutionError(resErr))
	}
	msg := fmt.Sprintf("%s (%d): %s", prefix, len(errs), strings.Join(details, "; "))
	return apierror.New(apierror.Invalid, msg).WithRetryable(apierror.False)
}

func formatResolutionError(resErr hub.ResolutionError) string {
	module := strings.Trim(resErr.Org+"/"+resErr.Name, "/")
	if module == "" {
		module = "unknown-module"
	}
	if resErr.Constraint != "" {
		module = module + "@" + resErr.Constraint
	}
	if resErr.Message != "" {
		return module + ": " + resErr.Message
	}
	return module
}
