// SPDX-License-Identifier: MPL-2.0

package hub

import (
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	lua "github.com/wippyai/go-lua"

	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/runtime/security"
)

// cacheEntry describes one cached artifact under the resolved vendor directory.
type cacheEntry struct {
	module  string
	version string
	relPath string
	size    int64
	pinned  bool
}

func (h *hubModule) cacheList(l *lua.LState) int {
	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.cache.list", "", nil) {
		return pushError(l, permissionDenied(l, "hub.cache.list", ""))
	}

	entries, _, cerr := collectCacheEntries()
	if cerr != nil {
		return pushError(l, hubCallError(l, cerr))
	}

	arr := lua.CreateTable(len(entries), 0)
	for i, e := range entries {
		arr.RawSetInt(i+1, cacheEntryToTable(e))
	}
	l.Push(arr)
	l.Push(lua.LNil)
	return 2
}

func (h *hubModule) cacheRemove(l *lua.LState) int {
	module := strings.TrimSpace(l.CheckString(1))
	version := strings.TrimSpace(l.CheckString(2))

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.cache.remove", module, nil) {
		return pushError(l, permissionDenied(l, "hub.cache.remove", module))
	}

	if module == "" || version == "" {
		return pushError(l, invalidArgument(l, "module and version required"))
	}

	force, err := parseForceOption(l, 3)
	if err != nil {
		return pushError(l, err)
	}

	name, parseErr := graph.ParseName(module)
	if parseErr != nil {
		return pushError(l, invalidArgument(l, parseErr.Error()))
	}

	lockObj, vendorDir, loadErr := loadLockAndVendor()
	if loadErr != nil {
		return pushError(l, hubCallError(l, loadErr))
	}

	if mod, ok := lockObj.GetModule(module); ok && mod.Version == version && !force {
		return pushError(l, lua.NewLuaError(l, "cannot remove lock-pinned artifact: "+module+"@"+version).
			WithKind(lua.Conflict).WithRetryable(false).WithDetails(map[string]any{
			"module":  module,
			"version": version,
		}))
	}

	target := filepath.Join(vendorDir, lock.WappPath(name, version))
	if !isWithinDir(vendorDir, target) {
		return pushError(l, invalidArgument(l, "module or version resolves outside the cache directory"))
	}
	if removeErr := os.RemoveAll(target); removeErr != nil {
		return pushError(l, hubCallError(l, removeErr))
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func (h *hubModule) cachePrune(l *lua.LState) int {
	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.cache.prune", "", nil) {
		return pushError(l, permissionDenied(l, "hub.cache.prune", ""))
	}

	dryRun, err := parseDryRunOption(l, 1)
	if err != nil {
		return pushError(l, err)
	}

	entries, vendorDir, cerr := collectCacheEntries()
	if cerr != nil {
		return pushError(l, hubCallError(l, cerr))
	}

	arr := lua.CreateTable(0, 0)
	count := 0
	for _, e := range entries {
		if e.pinned {
			continue
		}
		if !dryRun {
			target := filepath.Join(vendorDir, filepath.FromSlash(e.relPath))
			if removeErr := os.RemoveAll(target); removeErr != nil {
				return pushError(l, hubCallError(l, removeErr))
			}
		}
		count++
		arr.RawSetInt(count, cacheEntryToTable(e))
	}

	l.Push(arr)
	l.Push(lua.LNil)
	return 2
}

func cacheEntryToTable(e cacheEntry) *lua.LTable {
	t := lua.CreateTable(0, 4)
	t.RawSetString("module", lua.LString(e.module))
	t.RawSetString("version", lua.LString(e.version))
	t.RawSetString("size", lua.LNumber(e.size))
	t.RawSetString("pinned", lua.LBool(e.pinned))
	return t
}

// loadLockAndVendor loads the project lock file and resolves the vendor dir.
// A missing lock file yields an empty lock with default directories.
func loadLockAndVendor() (*lock.Lock, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	lockObj, err := lock.New(filepath.Join(cwd, lock.DefaultFilename))
	if err != nil {
		return nil, "", err
	}
	vendorDir := lock.ResolveLockPath(filepath.Dir(lockObj.Path()), lockObj.GetVendorPath())
	return lockObj, vendorDir, nil
}

// collectCacheEntries scans the vendor directory for cached .wapp artifacts and
// annotates each with lock-file pin status. Lock modules provide exact
// module/version for pinned artifacts; orphans are parsed from their path.
func collectCacheEntries() ([]cacheEntry, string, error) {
	lockObj, vendorDir, err := loadLockAndVendor()
	if err != nil {
		return nil, "", err
	}

	pinned := make(map[string]lock.Module)
	for _, mod := range lockObj.GetModules() {
		name, parseErr := graph.ParseName(mod.Name)
		if parseErr != nil {
			continue
		}
		pinned[filepath.ToSlash(lock.WappPath(name, mod.Version))] = mod
	}

	var entries []cacheEntry
	walkErr := filepath.WalkDir(vendorDir, func(p string, d iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".wapp") {
			return nil
		}

		rel, relErr := filepath.Rel(vendorDir, p)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)

		var size int64
		if info, infoErr := d.Info(); infoErr == nil {
			size = info.Size()
		}

		entry := cacheEntry{relPath: relSlash, size: size}
		if mod, ok := pinned[relSlash]; ok {
			entry.module = mod.Name
			entry.version = mod.Version
			entry.pinned = true
		} else {
			entry.module, entry.version = parseWappRelPath(relSlash)
		}
		entries = append(entries, entry)
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return nil, vendorDir, walkErr
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].module != entries[j].module {
			return entries[i].module < entries[j].module
		}
		return entries[i].version < entries[j].version
	})

	return entries, vendorDir, nil
}

// isWithinDir reports whether target resolves inside dir, guarding cache
// removal against module or version components that contain "..".
func isWithinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// parseWappRelPath reverses lock.WappPath ("org/module-version.wapp") into a
// module name and version, splitting at the semantic-version boundary so both
// hyphenated module names and prerelease versions round-trip.
func parseWappRelPath(rel string) (string, string) {
	rel = strings.TrimSuffix(rel, ".wapp")

	org := ""
	file := rel
	if slash := strings.LastIndex(rel, "/"); slash >= 0 {
		org = rel[:slash]
		file = rel[slash+1:]
	}

	module, version := splitModuleVersion(file)
	if org != "" {
		module = org + "/" + module
	}
	return strings.Trim(module, "/"), version
}

// splitModuleVersion splits "module-version" at the first hyphen whose suffix
// parses as a semantic version. Because both module names and semver
// prereleases contain hyphens (e.g. "my-module-v1.2.3-beta.1"), splitting on
// the final hyphen loses the prerelease; anchoring on the version parse
// round-trips both. Falls back to the final hyphen when no suffix is a valid
// version.
func splitModuleVersion(file string) (string, string) {
	for i := 0; i < len(file); i++ {
		if file[i] != '-' {
			continue
		}
		candidate := file[i+1:]
		if _, err := semver.NewVersion(candidate); err == nil {
			return file[:i], candidate
		}
	}
	if dash := strings.LastIndex(file, "-"); dash >= 0 {
		return file[:dash], file[dash+1:]
	}
	return file, ""
}

func parseForceOption(l *lua.LState, idx int) (bool, *lua.Error) {
	return parseBoolOption(l, idx, "force")
}

func parseDryRunOption(l *lua.LState, idx int) (bool, *lua.Error) {
	return parseBoolOption(l, idx, "dry_run")
}

func parseBoolOption(l *lua.LState, idx int, key string) (bool, *lua.Error) {
	if l.GetTop() < idx {
		return false, nil
	}
	val := l.Get(idx)
	if val == lua.LNil {
		return false, nil
	}
	tbl, ok := val.(*lua.LTable)
	if !ok {
		return false, invalidOptionError(l, "options", "table", val)
	}
	value, _, err := tableBool(l, tbl, key)
	if err != nil {
		return false, err
	}
	return value, nil
}
