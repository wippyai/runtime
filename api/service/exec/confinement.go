// SPDX-License-Identifier: MPL-2.0

package exec

import "slices"

// Confinement is the entry-owned ceiling for a confined executor. A nil
// Confinement leaves the executor's existing behavior unchanged. Platform
// implementations must reject every populated restriction they cannot enforce.
type Confinement struct {
	FS           *ConfinementFS          `json:"fs,omitempty"`
	Env          *ConfinementEnvironment `json:"env,omitempty"`
	Limits       *ConfinementLimits      `json:"limits,omitempty"`
	Tree         *ConfinementTree        `json:"tree,omitempty"`
	Home         string                  `json:"home,omitempty"`
	Network      string                  `json:"network,omitempty"`
	WorkDirRoots []string                `json:"work_dir_roots"`
}

// ConfinementFS contains subtree grants. When FS is present, an omitted
// access class denies that class; write also implies read.
type ConfinementFS struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
	Exec  []string `json:"exec,omitempty"`
}

type ConfinementEnvironment struct {
	Set   map[string]string `json:"set,omitempty"`
	Allow []string          `json:"allow,omitempty"`
}

type ConfinementLimits struct {
	MemoryMiB int64 `json:"mem_mb,omitempty"`
	PIDs      int64 `json:"pids,omitempty"`
	WallSec   int64 `json:"wall_s,omitempty"`
}

type ConfinementTree struct {
	KillOnOwnerExit bool `json:"kill_on_owner_exit,omitempty"`
}

// ConfinementPatch can only narrow an entry-owned Confinement. Pointer fields
// distinguish an omitted field (inherit) from a present empty list (deny all).
// Entry-only authority such as work_dir_roots, home and env.set is absent.
type ConfinementPatch struct {
	FS      *ConfinementFSPatch          `json:"fs,omitempty"`
	Env     *ConfinementEnvironmentPatch `json:"env,omitempty"`
	Network *string                      `json:"network,omitempty"`
	Limits  *ConfinementLimitsPatch      `json:"limits,omitempty"`
	Tree    *ConfinementTreePatch        `json:"tree,omitempty"`
}

type ConfinementFSPatch struct {
	Read  *[]string `json:"read,omitempty"`
	Write *[]string `json:"write,omitempty"`
	Exec  *[]string `json:"exec,omitempty"`
}

type ConfinementEnvironmentPatch struct {
	Allow *[]string `json:"allow,omitempty"`
}

type ConfinementLimitsPatch struct {
	MemoryMiB *int64 `json:"mem_mb,omitempty"`
	PIDs      *int64 `json:"pids,omitempty"`
	WallSec   *int64 `json:"wall_s,omitempty"`
}

type ConfinementTreePatch struct {
	KillOnOwnerExit *bool `json:"kill_on_owner_exit,omitempty"`
}

// Clone prevents a caller from changing a prepared process by mutating its
// launch patch after NewProcess returns.
func (p *ConfinementPatch) Clone() *ConfinementPatch {
	if p == nil {
		return nil
	}
	out := *p
	if p.FS != nil {
		fs := *p.FS
		fs.Read = cloneList(p.FS.Read)
		fs.Write = cloneList(p.FS.Write)
		fs.Exec = cloneList(p.FS.Exec)
		out.FS = &fs
	}
	if p.Env != nil {
		env := *p.Env
		env.Allow = cloneList(p.Env.Allow)
		out.Env = &env
	}
	if p.Network != nil {
		network := *p.Network
		out.Network = &network
	}
	if p.Limits != nil {
		limits := *p.Limits
		limits.MemoryMiB = cloneValue(p.Limits.MemoryMiB)
		limits.PIDs = cloneValue(p.Limits.PIDs)
		limits.WallSec = cloneValue(p.Limits.WallSec)
		out.Limits = &limits
	}
	if p.Tree != nil {
		tree := *p.Tree
		tree.KillOnOwnerExit = cloneValue(p.Tree.KillOnOwnerExit)
		out.Tree = &tree
	}
	return &out
}

func cloneList(values *[]string) *[]string {
	if values == nil {
		return nil
	}
	copy := slices.Clone(*values)
	if copy == nil {
		copy = []string{}
	}
	return &copy
}

func cloneValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
