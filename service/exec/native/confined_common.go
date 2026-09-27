// SPDX-License-Identifier: MPL-2.0

package native

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
)

// narrowConfinement applies the launch-owned patch to the entry ceiling and
// installs the resulting environment. Platform backends still have to bind
// paths, validate their host mechanisms, and install the policy before exec.
func narrowConfinement(process *ProcessExecutor, entry *execapi.Confinement, options execapi.ProcessOptions) (confinement.Policy, error) {
	base := confinement.FromEntry(entry)
	patch := confinement.FromPatch(options.Confine)
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
			key := normalizeEnvironmentName(name)
			for existing := range process.envs {
				if normalizeEnvironmentName(existing) == key {
					delete(process.envs, existing)
				}
			}
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
