// SPDX-License-Identifier: MPL-2.0

package native

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
)

// validateConfinementPolicy performs platform-independent admission before a
// backend probes or binds host mechanisms.
func validateConfinementPolicy(entry *execapi.Confinement) (confinement.Policy, error) {
	if err := entry.Validate(); err != nil {
		return confinement.Policy{}, err
	}
	policy := confinement.FromEntry(entry)
	if err := confinement.ValidateEntry(policy); err != nil {
		return confinement.Policy{}, execapi.NewInvalidConfinementError("confine")
	}
	return policy, nil
}

// selectConfinementRoot chooses the narrowest declared root containing path.
// Object identity remains owned by the platform-specific bound value.
func selectConfinementRoot[T any](roots map[string]T, path string) (T, bool) {
	var selected T
	selectedLength := -1
	for base, root := range roots {
		rel, err := filepath.Rel(base, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
			!filepath.IsAbs(rel) && len(base) > selectedLength {
			selected, selectedLength = root, len(base)
		}
	}
	return selected, selectedLength >= 0
}

// narrowConfinement applies the launch-owned patch to the entry ceiling and
// installs the resulting environment. Platform backends still have to bind
// paths, validate their host mechanisms, and install the policy before exec.
func narrowConfinement(process *ProcessExecutor, entry *execapi.Confinement, options execapi.ProcessOptions) (confinement.Policy, error) {
	base := confinement.FromEntry(entry)
	patch := confinement.FromPatch(options.Confine)
	normalizeConfinementEnvironment(&base, &patch)
	policy, err := confinement.Narrow(base, patch)
	if err != nil {
		if errors.Is(err, confinement.ErrWiden) {
			return confinement.Policy{}, execapi.ErrConfineWiden.WithCause(err)
		}
		return confinement.Policy{}, fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine"), err)
	}
	fs := policy.EffectiveFilesystem()
	privateTemp := slices.Contains(fs.Read.Paths, "{tmp}") ||
		slices.Contains(fs.Write.Paths, "{tmp}") ||
		slices.Contains(fs.Exec.Paths, "{tmp}")
	if err := applyConfinementEnvironment(process, policy.Env, policy.HomePrivate, privateTemp); err != nil {
		return confinement.Policy{}, err
	}
	if process.wd == "" || !policy.AllowsDeclaredWorkDir(process.wd) {
		return confinement.Policy{}, execapi.ErrConfineDenied
	}
	return policy, nil
}

// normalizeConfinementEnvironment applies the host's environment-name
// identity before comparing a launch patch with its entry ceiling. Windows
// treats PATH and Path as the same variable, while Unix does not.
func normalizeConfinementEnvironment(base *confinement.Policy, patch *confinement.Patch) {
	if base.Env != nil {
		for index, name := range base.Env.Allow {
			base.Env.Allow[index] = normalizeEnvironmentName(name)
		}
		set := make(map[string]string, len(base.Env.Set))
		for name, value := range base.Env.Set {
			set[normalizeEnvironmentName(name)] = value
		}
		base.Env.Set = set
	}
	if patch.EnvAllow != nil {
		for index, name := range *patch.EnvAllow {
			(*patch.EnvAllow)[index] = normalizeEnvironmentName(name)
		}
	}
}

// applyConfinementEnvironment validates the already-merged entry defaults and
// caller values, then installs entry-owned values. In particular, an entry
// default cannot quietly override a forced value: that would make a policy
// appear enforced while running with a different environment.
func applyConfinementEnvironment(process *ProcessExecutor, policy *confinement.Environment, homePrivate, tempPrivate bool) error {
	seen := make(map[string]struct{}, len(process.envs))
	allowed := make(map[string]struct{})
	pinned := make(map[string]struct{})
	if policy != nil {
		for _, name := range policy.Allow {
			allowed[normalizeEnvironmentName(name)] = struct{}{}
		}
		for name := range policy.Set {
			pinned[normalizeEnvironmentName(name)] = struct{}{}
		}
	}
	for name, value := range process.envs {
		if name == "" || strings.ContainsAny(name, "=\x00") || strings.ContainsRune(value, 0) {
			return execapi.NewInvalidConfinementError("confine.env")
		}
		key := normalizeEnvironmentName(name)
		if _, duplicate := seen[key]; duplicate {
			return execapi.NewInvalidConfinementError("confine.env")
		}
		seen[key] = struct{}{}
		if (homePrivate && (strings.EqualFold(name, "HOME") || strings.EqualFold(name, "USERPROFILE"))) ||
			(tempPrivate && (strings.EqualFold(name, "TMPDIR") || strings.EqualFold(name, "TMP") ||
				strings.EqualFold(name, "TEMP"))) {
			return execapi.NewInvalidConfinementError("confine.env")
		}
		if policy != nil {
			_, isPinned := pinned[key]
			_, isAllowed := allowed[key]
			if isPinned || !isAllowed {
				return execapi.NewInvalidConfinementError("confine.env")
			}
		}
	}
	if process.envs == nil {
		process.envs = make(map[string]string)
	}
	if policy != nil {
		for name, value := range policy.Set {
			process.envs[name] = value
		}
	}
	if tempPrivate {
		process.envs["TMPDIR"] = confinement.PrivateTempPath
	}
	if homePrivate {
		process.envs["HOME"] = confinement.PrivateHomePath
	}
	rebuildProcessEnvironment(process)
	return nil
}

func rebuildProcessEnvironment(process *ProcessExecutor) {
	names := make([]string, 0, len(process.envs))
	for name := range process.envs {
		names = append(names, name)
	}
	sort.Strings(names)
	process.cmd.Env = make([]string, 0, len(names))
	for _, name := range names {
		process.cmd.Env = append(process.cmd.Env, name+"="+process.envs[name])
	}
}
