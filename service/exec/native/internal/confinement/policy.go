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

// These directories exist only in a launch's private mount namespace. The
// prefix is reserved so a host bind grant can never alias either one.
const (
	PrivateRootPath = "/.wippy-confine-private"
	PrivateHomePath = PrivateRootPath + "/home"
	PrivateTempPath = PrivateRootPath + "/tmp"
)

// ExpandPrivatePaths gives placeholders stable, per-launch private-view names
// before path algebra runs. The platform backend must create these objects
// inside its private root; it must never bind matching host paths.
func ExpandPrivatePaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		switch path {
		case "{home}":
			out[i] = PrivateHomePath
		case "{tmp}":
			out[i] = PrivateTempPath
		default:
			out[i] = path
		}
	}
	return out
}

func ExpandPrivatePolicy(policy Policy) Policy {
	policy = clonePolicy(policy)
	if policy.FS != nil {
		policy.FS.Read.Paths = ExpandPrivatePaths(policy.FS.Read.Paths)
		policy.FS.Write.Paths = ExpandPrivatePaths(policy.FS.Write.Paths)
		policy.FS.Exec.Paths = ExpandPrivatePaths(policy.FS.Exec.Paths)
	}
	return policy
}

func ExpandPrivatePatch(patch Patch) Patch {
	if patch.FS == nil {
		return patch
	}
	fs := *patch.FS
	for _, access := range []**[]string{&fs.Read, &fs.Write, &fs.Exec} {
		if *access != nil {
			paths := ExpandPrivatePaths(**access)
			*access = &paths
		}
	}
	patch.FS = &fs
	return patch
}

// Access is either unrestricted or limited to declared clean absolute
// subtrees. Platform enforcement must bind those declarations to filesystem
// objects before launch. An empty restricted list denies the operation
// everywhere.
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
	Set   map[string]string
	Allow []string
}

type Limits struct {
	MemoryMiB int64
	PIDs      int64
	WallSec   int64
}

// Policy is the normalized lexical authority of an entry or launch. Narrow
// compares declared path spellings; the platform binder must separately pin
// and verify their filesystem identities before target execution.
type Policy struct {
	FS              *Filesystem
	Env             *Environment
	WorkDirRoots    []string
	Limits          Limits
	HomePrivate     bool
	NetworkNone     bool
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
	fs := effectiveFS(p)
	if p.FS != nil {
		// Validate the declared grants before normalizing write ⇒ read. The
		// union must not hide a malformed unrestricted grant with listed paths.
		fs = *p.FS
	}
	for _, class := range []struct {
		name   string
		access Access
	}{
		{"fs.read", fs.Read},
		{"fs.write", fs.Write},
		{"fs.exec", fs.Exec},
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
	if fs.Read.Unrestricted && fs.Write.Unrestricted && fs.Exec.Unrestricted &&
		p.Env == nil && !p.HomePrivate && !p.NetworkNone &&
		p.Limits == (Limits{}) && !p.KillOnOwnerExit {
		return fmt.Errorf("%w: no-op baseline", ErrInvalid)
	}
	return nil
}

// AllowsDeclaredWorkDir performs the lexical admission check for a working
// directory against entry-owned roots and read grants. A platform must still
// open the directory beneath a pinned entry root and verify its identity in
// the final filesystem view.
func (p Policy) AllowsDeclaredWorkDir(declared string) bool {
	if !pathWithin(declared, declared) {
		return false
	}
	inRoot := false
	for _, root := range p.WorkDirRoots {
		if pathWithin(declared, root) {
			inRoot = true
			break
		}
	}
	if !inRoot {
		return false
	}
	fs := effectiveFS(p)
	read := union(fs.Read, fs.Write)
	if read.Unrestricted {
		return true
	}
	for _, grant := range read.Paths {
		if pathWithin(declared, grant) {
			return true
		}
	}
	return false
}

// Narrow applies a launch patch. Path checks here are lexical policy
// comparisons, not filesystem authorization. The platform binds and verifies
// the resulting paths before launch.
func Narrow(base Policy, patch Patch) (Policy, error) {
	out := clonePolicy(base)
	baseFS := effectiveFS(base)
	outFS := effectiveFS(out)
	fsChanged := patch.FS != nil &&
		(patch.FS.Read != nil || patch.FS.Write != nil || patch.FS.Exec != nil)
	if fsChanged {
		applyPaths := func(dst *Access, requested *[]string) {
			if requested != nil {
				*dst = Access{Paths: slices.Clone(*requested)}
			}
		}
		applyPaths(&outFS.Read, patch.FS.Read)
		applyPaths(&outFS.Write, patch.FS.Write)
		applyPaths(&outFS.Exec, patch.FS.Exec)
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
	var err error
	out.Limits.MemoryMiB, err = narrowLimit(base.Limits.MemoryMiB, patch.Limits.MemoryMiB, "limits.mem_mb")
	if err != nil {
		return Policy{}, err
	}
	out.Limits.PIDs, err = narrowLimit(base.Limits.PIDs, patch.Limits.PIDs, "limits.pids")
	if err != nil {
		return Policy{}, err
	}
	out.Limits.WallSec, err = narrowLimit(base.Limits.WallSec, patch.Limits.WallSec, "limits.wall_s")
	if err != nil {
		return Policy{}, err
	}

	// A write grant also permits reads. If a patch narrows read but leaves a
	// broader write grant, rejecting it is safer than silently restoring read.
	if patch.FS != nil && patch.FS.Read != nil &&
		!subset(outFS.Write, outFS.Read) {
		return Policy{}, fmt.Errorf("%w: fs.read excludes effective fs.write", ErrInvalid)
	}
	outFS.Read = union(outFS.Read, outFS.Write)
	baseRead := union(baseFS.Read, baseFS.Write)
	if !subset(outFS.Read, baseRead) ||
		!subset(outFS.Write, baseFS.Write) ||
		!subset(outFS.Exec, baseFS.Exec) {
		return Policy{}, fmt.Errorf("%w: fs", ErrWiden)
	}
	if out.FS != nil || fsChanged {
		out.FS = &outFS
	}
	return out, nil
}

func narrowLimit(base int64, requested *int64, name string) (int64, error) {
	if requested == nil {
		return base, nil
	}
	if *requested <= 0 {
		return 0, fmt.Errorf("%w: %s must be positive", ErrInvalid, name)
	}
	if base > 0 && *requested > base {
		return 0, fmt.Errorf("%w: %s", ErrWiden, name)
	}
	return *requested, nil
}

func clonePolicy(p Policy) Policy {
	p.WorkDirRoots = slices.Clone(p.WorkDirRoots)
	if p.FS != nil {
		fs := *p.FS
		fs.Read.Paths = slices.Clone(fs.Read.Paths)
		fs.Write.Paths = slices.Clone(fs.Write.Paths)
		fs.Exec.Paths = slices.Clone(fs.Exec.Paths)
		p.FS = &fs
	}
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

func effectiveFS(p Policy) Filesystem {
	if p.FS != nil {
		fs := *p.FS
		fs.Read = union(fs.Read, fs.Write)
		return fs
	}
	return Filesystem{
		Read:  Access{Unrestricted: true},
		Write: Access{Unrestricted: true},
		Exec:  Access{Unrestricted: true},
	}
}

// EffectiveFilesystem returns a copied, normalized filesystem policy. The
// platform backend uses it after Narrow to compile mount and Landlock grants.
func (p Policy) EffectiveFilesystem() Filesystem {
	fs := effectiveFS(p)
	fs.Read.Paths = slices.Clone(fs.Read.Paths)
	fs.Write.Paths = slices.Clone(fs.Write.Paths)
	fs.Exec.Paths = slices.Clone(fs.Exec.Paths)
	return fs
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

// pathWithin compares clean absolute policy spellings. It is not a substitute
// for openat2/handle-based resolution and grants no filesystem authority.
func pathWithin(requested, grant string) bool {
	requestedPrivate := requested == "{home}" || requested == "{tmp}"
	grantPrivate := grant == "{home}" || grant == "{tmp}"
	if requestedPrivate || grantPrivate {
		return requestedPrivate && grantPrivate && requested == grant
	}
	if !filepath.IsAbs(requested) || !filepath.IsAbs(grant) ||
		filepath.Clean(requested) != requested || filepath.Clean(grant) != grant {
		return false
	}
	rel, err := filepath.Rel(grant, requested)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
