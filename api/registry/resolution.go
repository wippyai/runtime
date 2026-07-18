// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// ErrDependencyResolutionNotFound means a history version predates durable
// dependency resolutions. Callers may use the legacy resolver once and
// checkpoint the result, but must not treat any other storage error as legacy.
var ErrDependencyResolutionNotFound = errors.New("dependency resolution not found")

// ErrInvalidDependencyResolution means a caller supplied a graph that cannot
// be made into a durable snapshot without losing dependency semantics.
var ErrInvalidDependencyResolution = errors.New("invalid dependency resolution")

// ResolvedModule is an immutable module selection stored with a registry
// version. Download URLs are intentionally excluded because they expire; a
// missing artifact is fetched again by exact version/digest.
type ResolvedModule struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	VersionID string `json:"version_id,omitempty"`
	Source    string `json:"source,omitempty"`
	Digest    string `json:"digest,omitempty"`
	SizeBytes uint64 `json:"size_bytes,omitempty"`
	Protected bool   `json:"protected,omitempty"`
}

// DependencyRoot records one authored dependency declaration used as a solver
// input. Parameters remain in the ordinary registry changeset because they
// configure linking rather than version selection.
type DependencyRoot struct {
	ID        string `json:"id"`
	Component string `json:"component"`
	Version   string `json:"version"`
}

// DependencyResolution is the exact graph selected for a registry version.
// InputDigest identifies the declared root set; Digest identifies the complete
// immutable selection independently of mutable download URLs.
type DependencyResolution struct {
	Digest         string           `json:"digest"`
	InputDigest    string           `json:"input_digest"`
	BaselineDigest string           `json:"baseline_digest,omitempty"`
	Roots          []DependencyRoot `json:"roots"`
	Modules        []ResolvedModule `json:"modules"`
}

// CanRebaseDependencyResolution reports whether next may replace the graph
// checkpoint for the same declarative registry version. A registry version is
// immutable within one deployment baseline, but its effective graph must be
// recomputed when that independently versioned baseline changes. An unbound
// legacy graph may be upgraded once to a baseline-bound graph.
func CanRebaseDependencyResolution(existing, next *DependencyResolution) bool {
	if existing == nil || next == nil || !existing.Valid() || !next.Valid() {
		return false
	}
	if next.BaselineDigest == "" || existing.Digest == next.Digest {
		return false
	}
	return existing.BaselineDigest == "" || existing.BaselineDigest != next.BaselineDigest
}

// Canonical returns a detached, deterministically ordered resolution and
// recomputes its content digest.
func (r *DependencyResolution) Canonical() *DependencyResolution {
	if r == nil {
		return nil
	}
	out := &DependencyResolution{
		InputDigest:    r.InputDigest,
		BaselineDigest: r.BaselineDigest,
		Roots:          append([]DependencyRoot(nil), r.Roots...),
		Modules:        append([]ResolvedModule(nil), r.Modules...),
	}
	sort.Slice(out.Roots, func(i, j int) bool {
		if out.Roots[i].ID != out.Roots[j].ID {
			return out.Roots[i].ID < out.Roots[j].ID
		}
		if out.Roots[i].Component != out.Roots[j].Component {
			return out.Roots[i].Component < out.Roots[j].Component
		}
		return out.Roots[i].Version < out.Roots[j].Version
	})
	sort.Slice(out.Modules, func(i, j int) bool {
		left, right := out.Modules[i], out.Modules[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		if left.VersionID != right.VersionID {
			return left.VersionID < right.VersionID
		}
		if left.Digest != right.Digest {
			return left.Digest < right.Digest
		}
		if left.SizeBytes != right.SizeBytes {
			return left.SizeBytes < right.SizeBytes
		}
		return !left.Protected && right.Protected
	})
	out.Digest = out.computeDigest()
	return out
}

// Valid reports whether the stored digest matches the canonical resolution.
func (r *DependencyResolution) Valid() bool {
	if r == nil || r.Digest == "" {
		return false
	}
	rootIDs := make(map[string]struct{}, len(r.Roots))
	components := make(map[string]struct{}, len(r.Roots))
	for _, root := range r.Roots {
		if root.ID == "" || root.Component == "" || root.Version == "" {
			return false
		}
		if _, duplicate := rootIDs[root.ID]; duplicate {
			return false
		}
		if _, duplicate := components[root.Component]; duplicate {
			return false
		}
		rootIDs[root.ID] = struct{}{}
		components[root.Component] = struct{}{}
	}
	modules := make(map[string]struct{}, len(r.Modules))
	for _, module := range r.Modules {
		if module.Name == "" || module.Version == "" || module.Digest == "" {
			return false
		}
		if _, duplicate := modules[module.Name]; duplicate {
			return false
		}
		modules[module.Name] = struct{}{}
	}
	return r.Digest == r.Canonical().Digest
}

func (r *DependencyResolution) computeDigest() string {
	payload := struct {
		InputDigest    string           `json:"input_digest"`
		BaselineDigest string           `json:"baseline_digest,omitempty"`
		Roots          []DependencyRoot `json:"roots"`
		Modules        []ResolvedModule `json:"modules"`
	}{
		InputDigest:    r.InputDigest,
		BaselineDigest: r.BaselineDigest,
		Roots:          r.Roots,
		Modules:        r.Modules,
	}
	data, _ := json.Marshal(payload) // Struct contains only JSON-safe primitives.
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ResolutionHistory atomically stores a changeset and the exact dependency
// graph selected while applying it. Implementations must write the version,
// changeset, resolution reference, and head update in one transaction.
type ResolutionHistory interface {
	History
	GetDependencyResolution(Version) (*DependencyResolution, error)
	SaveWithDependencyResolution(Version, ChangeSet, *DependencyResolution, bool) error
	CheckpointDependencyResolution(Version, *DependencyResolution) error
}

// ResolutionHeadCASHistory attaches an exact graph and moves head in one
// atomic operation. A failed head comparison must leave the target version
// unmodified, so a losing rollback cannot freeze a graph that was never live.
type ResolutionHeadCASHistory interface {
	ResolutionHistory
	// CompareAndSetHeadWithDependencyResolution atomically moves the history
	// head and checkpoints the effective graph. It may rebind an existing
	// version only when CanRebaseDependencyResolution permits a deployment
	// baseline transition.
	CompareAndSetHeadWithDependencyResolution(expected, target Version, resolution *DependencyResolution) error
}
