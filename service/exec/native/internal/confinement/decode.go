// SPDX-License-Identifier: MPL-2.0

package confinement

import execapi "github.com/wippyai/runtime/api/service/exec"

// FromEntry copies the public registry shape into the internal policy model.
// It deliberately does not resolve placeholders or bind paths: those are
// launch-specific operations performed by the platform backend.
func FromEntry(entry *execapi.Confinement) Policy {
	if entry == nil {
		return Policy{}
	}
	policy := Policy{
		WorkDirRoots: append([]string(nil), entry.WorkDirRoots...),
		HomePrivate:  entry.Home == "private",
		NetworkNone:  entry.Network == "none",
	}
	if entry.FS != nil {
		policy.FS = &Filesystem{
			Read:  Access{Paths: append([]string(nil), entry.FS.Read...)},
			Write: Access{Paths: append([]string(nil), entry.FS.Write...)},
			Exec:  Access{Paths: append([]string(nil), entry.FS.Exec...)},
		}
	}
	if entry.Env != nil {
		policy.Env = &Environment{
			Allow: append([]string(nil), entry.Env.Allow...),
			Set:   make(map[string]string, len(entry.Env.Set)),
		}
		for name, value := range entry.Env.Set {
			policy.Env.Set[name] = value
		}
	}
	if entry.Limits != nil {
		policy.Limits = Limits{
			MemoryMiB: entry.Limits.MemoryMiB,
			PIDs:      entry.Limits.PIDs,
			WallSec:   entry.Limits.WallSec,
		}
	}
	if entry.Tree != nil {
		policy.KillOnOwnerExit = entry.Tree.KillOnOwnerExit
	}
	return policy
}

// FromPatch preserves the omitted-versus-empty distinction of the public
// launch shape. Narrow is responsible for checking every changed authority.
func FromPatch(input *execapi.ConfinementPatch) Patch {
	if input == nil {
		return Patch{}
	}
	patch := Patch{NetworkNone: nil}
	if input.FS != nil {
		patch.FS = &PathsPatch{
			Read:  clonePathPatch(input.FS.Read),
			Write: clonePathPatch(input.FS.Write),
			Exec:  clonePathPatch(input.FS.Exec),
		}
	}
	if input.Env != nil {
		patch.EnvAllow = clonePathPatch(input.Env.Allow)
	}
	if input.Network != nil {
		none := *input.Network == "none"
		patch.NetworkNone = &none
	}
	if input.Limits != nil {
		patch.Limits = LimitsPatch{
			MemoryMiB: cloneInt(input.Limits.MemoryMiB),
			PIDs:      cloneInt(input.Limits.PIDs),
			WallSec:   cloneInt(input.Limits.WallSec),
		}
	}
	if input.Tree != nil {
		patch.KillOnOwnerExit = cloneBool(input.Tree.KillOnOwnerExit)
	}
	return patch
}

func clonePathPatch(paths *[]string) *[]string {
	if paths == nil {
		return nil
	}
	clone := append([]string{}, (*paths)...)
	return &clone
}

func cloneInt(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
