// SPDX-License-Identifier: MPL-2.0

// Package confinement contains the pure policy algebra for native execution.
// It does not resolve paths, authorize a caller, or enforce a policy. A launch
// must not consume its result until those steps are implemented and verified.
package confinement

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

var (
	ErrInvalid = errors.New("invalid confinement policy")
	ErrWiden   = errors.New("confinement patch widens entry policy")
)

// Access is either unrestricted or limited to the listed canonical subtrees.
// An empty restricted list denies the operation everywhere.
type Access struct {
	Paths        []string
	Unrestricted bool
}

type Filesystem struct {
	Read  Access
	Write Access
	Exec  Access
}

type Environment struct {
	Allow []string
	Set   map[string]string
}

type Limits struct {
	MemoryMiB int64
	PIDs      int64
	WallSec   int64
}

// Policy is the normalized authority of an entry or launch. All Paths must
// already refer to bound, canonical objects before a caller relies on Narrow.
type Policy struct {
	WorkDirRoots    []string
	FS              Filesystem
	Env             *Environment
	HomePrivate     bool
	NetworkNone     bool
	Limits          Limits
	KillOnOwnerExit bool
}

// PathsPatch uses nil to inherit and a non-nil empty slice to deny all.
type PathsPatch struct {
	Read  *[]string
	Write *[]string
	Exec  *[]string
}

type LimitsPatch struct {
	MemoryMiB *int64
	PIDs      *int64
	WallSec   *int64
}

// Patch can only remove authority from a Policy. Entry-owned roots, home and
// forced environment values deliberately have no patch representation.
type Patch struct {
	FS              *PathsPatch
	EnvAllow        *[]string
	NetworkNone     *bool
	Limits          LimitsPatch
	KillOnOwnerExit *bool
}

// ValidateEntry rejects malformed or deceptive entry ceilings. It does not
// probe host mechanisms; a valid policy may still be unsupported at launch.
func ValidateEntry(p Policy) error {
	if len(p.WorkDirRoots) == 0 {
		return fmt.Errorf("%w: work_dir_roots is required", ErrInvalid)
	}
	for _, root := range p.WorkDirRoots {
		if !pathWithin(root, root) {
			return fmt.Errorf("%w: work_dir_root %q", ErrInvalid, root)
		}
	}
	for _, class := range []struct {
		name   string
		access Access
	}{
		{"fs.read", p.FS.Read},
		{"fs.write", p.FS.Write},
		{"fs.exec", p.FS.Exec},
	} {
		if class.access.Unrestricted && len(class.access.Paths) > 0 {
			return fmt.Errorf("%w: %s has unrestricted and listed grants", ErrInvalid, class.name)
		}
		for _, grant := range class.access.Paths {
			if !pathWithin(grant, grant) {
				return fmt.Errorf("%w: %s path %q", ErrInvalid, class.name, grant)
			}
		}
	}
	if !subset(p.FS.Write, p.FS.Read) {
		return fmt.Errorf("%w: fs.read excludes fs.write", ErrInvalid)
	}
	for _, limit := range []struct {
		name  string
		value int64
	}{
		{"limits.mem_mb", p.Limits.MemoryMiB},
		{"limits.pids", p.Limits.PIDs},
		{"limits.wall_s", p.Limits.WallSec},
	} {
		if limit.value < 0 {
			return fmt.Errorf("%w: %s must not be negative", ErrInvalid, limit.name)
		}
	}
	if p.FS.Read.Unrestricted && p.FS.Write.Unrestricted && p.FS.Exec.Unrestricted &&
		p.Env == nil && !p.HomePrivate && !p.NetworkNone &&
		p.Limits == (Limits{}) && !p.KillOnOwnerExit {
		return fmt.Errorf("%w: no-op baseline", ErrInvalid)
	}
	return nil
}

// AllowsBoundWorkDir checks the effective working directory against both the
// entry-owned roots and read grants. bound must be the canonical path of the
// securely opened directory; this function must never be called with a raw
// caller-supplied work_dir string as its only authorization check.
func (p Policy) AllowsBoundWorkDir(bound string) bool {
	if !pathWithin(bound, bound) {
		return false
	}
	inRoot := false
	for _, root := range p.WorkDirRoots {
		if pathWithin(bound, root) {
			inRoot = true
			break
		}
	}
	if !inRoot {
		return false
	}
	read := union(p.FS.Read, p.FS.Write)
	if read.Unrestricted {
		return true
	}
	for _, grant := range read.Paths {
		if pathWithin(bound, grant) {
			return true
		}
	}
	return false
}

// Narrow applies a launch patch. Path checks here are policy comparisons, not
// filesystem authorization: the caller must bind and verify every path first.
func Narrow(base Policy, patch Patch) (Policy, error) {
	out := clonePolicy(base)
	if patch.FS != nil {
		applyPaths := func(dst *Access, requested *[]string) {
			if requested != nil {
				*dst = Access{Paths: slices.Clone(*requested)}
			}
		}
		applyPaths(&out.FS.Read, patch.FS.Read)
		applyPaths(&out.FS.Write, patch.FS.Write)
		applyPaths(&out.FS.Exec, patch.FS.Exec)
	}

	if patch.EnvAllow != nil {
		if base.Env == nil {
			out.Env = &Environment{Allow: slices.Clone(*patch.EnvAllow)}
		} else {
			for _, name := range *patch.EnvAllow {
				if !slices.Contains(base.Env.Allow, name) {
					return Policy{}, fmt.Errorf("%w: env.allow %q", ErrWiden, name)
				}
			}
			out.Env.Allow = slices.Clone(*patch.EnvAllow)
		}
	}
	if patch.NetworkNone != nil {
		if !*patch.NetworkNone {
			return Policy{}, fmt.Errorf("%w: network", ErrWiden)
		}
		out.NetworkNone = true
	}
	if patch.KillOnOwnerExit != nil {
		if !*patch.KillOnOwnerExit && base.KillOnOwnerExit {
			return Policy{}, fmt.Errorf("%w: tree.kill_on_owner_exit", ErrWiden)
		}
		out.KillOnOwnerExit = *patch.KillOnOwnerExit
	}
	for _, limit := range []struct {
		name      string
		base      int64
		requested *int64
		set       func(int64)
	}{
		{"limits.mem_mb", base.Limits.MemoryMiB, patch.Limits.MemoryMiB, func(v int64) { out.Limits.MemoryMiB = v }},
		{"limits.pids", base.Limits.PIDs, patch.Limits.PIDs, func(v int64) { out.Limits.PIDs = v }},
		{"limits.wall_s", base.Limits.WallSec, patch.Limits.WallSec, func(v int64) { out.Limits.WallSec = v }},
	} {
		if limit.requested == nil {
			continue
		}
		if *limit.requested <= 0 {
			return Policy{}, fmt.Errorf("%w: %s must be positive", ErrInvalid, limit.name)
		}
		if limit.base > 0 && *limit.requested > limit.base {
			return Policy{}, fmt.Errorf("%w: %s", ErrWiden, limit.name)
		}
		limit.set(*limit.requested)
	}

	// A write grant also permits reads. If a patch narrows read but leaves a
	// broader write grant, rejecting it is safer than silently restoring read.
	if patch.FS != nil && patch.FS.Read != nil &&
		!subset(out.FS.Write, out.FS.Read) {
		return Policy{}, fmt.Errorf("%w: fs.read excludes effective fs.write", ErrInvalid)
	}
	out.FS.Read = union(out.FS.Read, out.FS.Write)
	baseRead := union(base.FS.Read, base.FS.Write)
	if !subset(out.FS.Read, baseRead) ||
		!subset(out.FS.Write, base.FS.Write) ||
		!subset(out.FS.Exec, base.FS.Exec) {
		return Policy{}, fmt.Errorf("%w: fs", ErrWiden)
	}
	return out, nil
}

func clonePolicy(p Policy) Policy {
	p.WorkDirRoots = slices.Clone(p.WorkDirRoots)
	p.FS.Read.Paths = slices.Clone(p.FS.Read.Paths)
	p.FS.Write.Paths = slices.Clone(p.FS.Write.Paths)
	p.FS.Exec.Paths = slices.Clone(p.FS.Exec.Paths)
	if p.Env != nil {
		env := *p.Env
		env.Allow = slices.Clone(env.Allow)
		env.Set = make(map[string]string, len(p.Env.Set))
		for name, value := range p.Env.Set {
			env.Set[name] = value
		}
		p.Env = &env
	}
	return p
}

func union(a, b Access) Access {
	if a.Unrestricted || b.Unrestricted {
		return Access{Unrestricted: true}
	}
	out := Access{Paths: slices.Clone(a.Paths)}
	for _, path := range b.Paths {
		if !slices.Contains(out.Paths, path) {
			out.Paths = append(out.Paths, path)
		}
	}
	return out
}

func subset(candidate, ceiling Access) bool {
	for _, requested := range candidate.Paths {
		if !pathWithin(requested, requested) {
			return false
		}
	}
	if ceiling.Unrestricted {
		return true
	}
	if candidate.Unrestricted {
		return false
	}
	for _, requested := range candidate.Paths {
		contained := false
		for _, grant := range ceiling.Paths {
			if pathWithin(requested, grant) {
				contained = true
				break
			}
		}
		if !contained {
			return false
		}
	}
	return true
}

// pathWithin is only meaningful after both operands were securely bound and
// canonicalized. It is not a substitute for openat2/handle-based resolution.
func pathWithin(requested, grant string) bool {
	if !filepath.IsAbs(requested) || !filepath.IsAbs(grant) ||
		filepath.Clean(requested) != requested || filepath.Clean(grant) != grant {
		return false
	}
	rel, err := filepath.Rel(grant, requested)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
