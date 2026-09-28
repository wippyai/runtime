// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"math"
	"path/filepath"
	"runtime"
	"strings"
)

// Validate checks the entry's declared ceiling without probing the host. The
// executor must separately prove that it can enforce the policy before Add.
func (c *Confinement) Validate() error {
	if c == nil {
		return nil
	}
	if err := validateConfinementRoots(c.WorkDirRoots); err != nil {
		return err
	}
	if c.FS != nil {
		if err := validateConfinementPaths(c.FS.Read, "confine.fs.read"); err != nil {
			return err
		}
		if err := validateConfinementPaths(c.FS.Write, "confine.fs.write"); err != nil {
			return err
		}
		if err := validateConfinementPaths(c.FS.Exec, "confine.fs.exec"); err != nil {
			return err
		}
	}
	if c.Home != "" && c.Home != "private" {
		return NewInvalidConfinementError("confine.home")
	}
	if err := validateConfinementEnvironment(c.Env); err != nil {
		return err
	}
	if c.Network != "" && c.Network != "none" {
		return NewInvalidConfinementError("confine.network")
	}
	if !validConfinementLimits(c.Limits) {
		return NewInvalidConfinementError("confine.limits")
	}
	if c.FS == nil && c.Home == "" && c.Env == nil && c.Network == "" &&
		(c.Limits == nil || *c.Limits == (ConfinementLimits{})) &&
		(c.Tree == nil || !c.Tree.KillOnOwnerExit) {
		return NewInvalidConfinementError("confine")
	}
	return nil
}

func validateConfinementRoots(roots []string) error {
	if len(roots) == 0 {
		return NewInvalidConfinementError("confine.work_dir_roots")
	}
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if !validConfinementPath(root, false) {
			return NewInvalidConfinementError("confine.work_dir_roots")
		}
		key := root
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(root)
		}
		if _, exists := seen[key]; exists {
			return NewInvalidConfinementError("confine.work_dir_roots")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateConfinementEnvironment(env *ConfinementEnvironment) error {
	if env == nil {
		return nil
	}
	if err := validateConfinementEnvNames(env.Allow, "confine.env.allow"); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(env.Set))
	for name, value := range env.Set {
		key := name
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(name)
		}
		if !validConfinementEnvName(name) || containsNUL(value) || reservedConfinementEnvName(name) {
			return NewInvalidConfinementError("confine.env.set")
		}
		if _, exists := seen[key]; exists {
			return NewInvalidConfinementError("confine.env.set")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validConfinementLimits(limits *ConfinementLimits) bool {
	return limits == nil ||
		(limits.MemoryMiB >= 0 && limits.MemoryMiB <= math.MaxInt64/(1<<20) &&
			limits.PIDs >= 0 && limits.WallSec >= 0 && limits.WallSec <= math.MaxInt64/int64(1e9))
}

// Validate checks a narrowing patch's shape. This does not compare the patch
// to the entry baseline or prove any OS mechanism is installed.
func (p *ConfinementPatch) Validate() error {
	if p == nil {
		return nil
	}
	if p.FS != nil {
		for _, class := range []struct {
			paths *[]string
			field string
		}{
			{p.FS.Read, "confine.fs.read"},
			{p.FS.Write, "confine.fs.write"},
			{p.FS.Exec, "confine.fs.exec"},
		} {
			if class.paths != nil {
				if err := validateConfinementPaths(*class.paths, class.field); err != nil {
					return err
				}
			}
		}
	}
	if p.Env != nil && p.Env.Allow != nil {
		if err := validateConfinementEnvNames(*p.Env.Allow, "confine.env.allow"); err != nil {
			return err
		}
	}
	if p.Network != nil && *p.Network != "none" {
		return NewInvalidConfinementError("confine.network")
	}
	if p.Limits != nil {
		for _, limit := range []struct {
			value *int64
			field string
			max   int64
		}{
			{p.Limits.MemoryMiB, "confine.limits.mem_mb", math.MaxInt64 / (1 << 20)},
			{p.Limits.PIDs, "confine.limits.pids", math.MaxInt64},
			{p.Limits.WallSec, "confine.limits.wall_s", math.MaxInt64 / int64(1e9)},
		} {
			if limit.value != nil && (*limit.value <= 0 || *limit.value > limit.max) {
				return NewInvalidConfinementError(limit.field)
			}
		}
	}
	return nil
}

func validateConfinementPaths(paths []string, field string) error {
	for _, path := range paths {
		if !validConfinementPath(path, true) {
			return NewInvalidConfinementError(field)
		}
	}
	return nil
}

func validConfinementPath(path string, placeholders bool) bool {
	if placeholders && (path == "{home}" || path == "{tmp}") {
		return true
	}
	if path == "" || strings.ContainsAny(path, "{}\x00") ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	// Device and UNC paths have different authority and network semantics from
	// local volume paths. Do not admit them until the Windows binder proves them.
	if runtime.GOOS == "windows" && (strings.HasPrefix(path, `\\`) ||
		strings.Count(path, ":") != 1) {
		return false
	}
	return true
}

func validateConfinementEnvNames(names []string, field string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validConfinementEnvName(name) || reservedConfinementEnvName(name) {
			return NewInvalidConfinementError(field)
		}
		key := name
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(name)
		}
		if _, exists := seen[key]; exists {
			return NewInvalidConfinementError(field)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validConfinementEnvName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "=\x00")
}

func containsNUL(value string) bool { return strings.ContainsRune(value, 0) }

func allowedConfinementEnvName(allow []string, name string) bool {
	for _, candidate := range allow {
		if sameConfinementEnvName(candidate, name) {
			return true
		}
	}
	return false
}

func pinnedConfinementEnvName(set map[string]string, name string) bool {
	for candidate := range set {
		if sameConfinementEnvName(candidate, name) {
			return true
		}
	}
	return false
}

func sameConfinementEnvName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func reservedConfinementEnvName(name string) bool {
	for _, reserved := range []string{"HOME", "TMPDIR", "TMP", "TEMP", "USERPROFILE"} {
		if strings.EqualFold(name, reserved) {
			return true
		}
	}
	return false
}
