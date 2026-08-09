// SPDX-License-Identifier: MPL-2.0

// Package artifact defines format discovery, validation, and materialization
// for filesystem resources carried by WAPPs.
//
// Artifacts remain ordinary WAPP filesystem resources. A resource opts into
// format-specific handling through meta.artifact.format. Module selection,
// downloading, integrity verification, and dependency resolution stay with
// their existing owners.
package artifact

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/wapp"
)

const MetadataKey = "artifact"

var (
	ErrDuplicateFormat = errors.New("artifact format already registered")
	ErrUnknownFormat   = errors.New("unknown artifact format")
)

// Declaration is the authored metadata attached to an embedded filesystem.
type Declaration struct {
	Format string
}

// Descriptor is format-derived identity used to choose a stable materialized
// location. Identity and Version come from the artifact contents, not authored
// resource metadata.
type Descriptor struct {
	Identity     string
	Version      string
	RelativePath string
}

// InspectInput provides immutable module and resource context to a format.
type InspectInput struct {
	Filesystem    fs.FS
	ModuleVersion string
	ResourceID    wapp.ID
}

// Format validates one artifact filesystem and derives its identity and stable
// materialization path. Root names the format-managed subtree below the
// configured artifact root; exact reconciliation may replace that entire
// subtree. Formats do not download modules, invoke package managers, mutate
// locks, or register themselves globally.
type Format interface {
	Name() string
	Root() string
	Inspect(context.Context, InspectInput) (Descriptor, error)
}

// Registry contains the formats available to one explicitly composed caller.
// Construction is explicit so commands, boot, and tests do not share globals.
type Registry struct {
	formats map[string]Format
}

func NewRegistry() *Registry {
	return &Registry{formats: make(map[string]Format)}
}

func (r *Registry) Register(format Format) error {
	if format == nil {
		return errors.New("artifact format is nil")
	}
	name := strings.TrimSpace(format.Name())
	if name == "" {
		return errors.New("artifact format name is empty")
	}
	root := strings.TrimSpace(format.Root())
	if err := validatePortablePath(root); err != nil {
		return fmt.Errorf("artifact format %q has invalid root: %w", name, err)
	}

	if _, exists := r.formats[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateFormat, name)
	}
	for registeredName, registered := range r.formats {
		registeredRoot := path.Clean(registered.Root())
		if root != registeredRoot &&
			(strings.HasPrefix(root, registeredRoot+"/") ||
				strings.HasPrefix(registeredRoot, root+"/")) {
			return fmt.Errorf(
				"artifact format roots overlap: %q owns %q and %q owns %q",
				registeredName, registeredRoot, name, root,
			)
		}
	}
	r.formats[name] = format
	return nil
}

func (r *Registry) Resolve(name string) (Format, bool) {
	if r == nil {
		return nil, false
	}
	format, ok := r.formats[name]
	return format, ok
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.formats))
	for name := range r.formats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Roots returns the non-overlapping materialization subtrees owned by the
// registered formats. Multiple formats may intentionally share one root.
func (r *Registry) Roots() []string {
	if r == nil {
		return nil
	}
	unique := make(map[string]struct{}, len(r.formats))
	for _, format := range r.formats {
		unique[path.Clean(format.Root())] = struct{}{}
	}
	roots := make([]string, 0, len(unique))
	for root := range unique {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

// ParseDeclaration reads meta.artifact.format. Metadata without an artifact
// key is not an artifact. Once the key exists, malformed declarations fail
// closed.
func ParseDeclaration(meta wapp.Metadata) (Declaration, bool, error) {
	raw, exists := meta[MetadataKey]
	if !exists {
		return Declaration{}, false, nil
	}

	block, ok := stringMap(raw)
	if !ok {
		return Declaration{}, true, errors.New("meta.artifact must be an object")
	}
	rawFormat, exists := block["format"]
	if !exists {
		return Declaration{}, true, errors.New("meta.artifact.format is required")
	}
	format, ok := rawFormat.(string)
	if !ok || strings.TrimSpace(format) == "" {
		return Declaration{}, true, errors.New("meta.artifact.format must be a non-empty string")
	}
	return Declaration{Format: strings.TrimSpace(format)}, true, nil
}

func stringMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case attrs.Bag:
		return map[string]any(typed), true
	case wapp.Metadata:
		return map[string]any(typed), true
	default:
		return nil, false
	}
}

// Inspect resolves and invokes the declared format.
func (r *Registry) Inspect(ctx context.Context, declaration Declaration, input InspectInput) (Descriptor, error) {
	format, ok := r.Resolve(declaration.Format)
	if !ok {
		return Descriptor{}, fmt.Errorf("%w %q for artifact %s (registered: %s)",
			ErrUnknownFormat, declaration.Format, input.ResourceID.String(),
			strings.Join(r.Names(), ", "))
	}
	descriptor, err := format.Inspect(ctx, input)
	if err != nil {
		return Descriptor{}, fmt.Errorf("validate %s artifact %s: %w",
			declaration.Format, input.ResourceID.String(), err)
	}
	if strings.TrimSpace(descriptor.Identity) == "" {
		return Descriptor{}, errors.New("artifact format returned empty identity")
	}
	if err := validatePortablePath(descriptor.RelativePath); err != nil {
		return Descriptor{}, fmt.Errorf("artifact format returned invalid relative path: %w", err)
	}
	descriptor.RelativePath = path.Clean(descriptor.RelativePath)
	root := path.Clean(format.Root())
	if descriptor.RelativePath != root &&
		!strings.HasPrefix(descriptor.RelativePath, root+"/") {
		return Descriptor{}, fmt.Errorf(
			"artifact format %q returned path %q outside its root %q",
			declaration.Format, descriptor.RelativePath, root,
		)
	}
	return descriptor, nil
}

func validatePortablePath(value string) error {
	if value == "" {
		return errors.New("path is empty")
	}
	if !fs.ValidPath(value) || value == "." {
		return errors.New("path must be a canonical relative slash path")
	}
	if strings.ContainsAny(value, `\:`) {
		return errors.New("path contains a platform-specific separator or volume")
	}
	for _, segment := range strings.Split(value, "/") {
		if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return fmt.Errorf("path segment %q has a non-portable suffix", segment)
		}
		base := strings.ToLower(strings.SplitN(segment, ".", 2)[0])
		if isWindowsReservedName(base) {
			return fmt.Errorf("path segment %q is a reserved name", segment)
		}
	}
	return nil
}

func isWindowsReservedName(name string) bool {
	switch name {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return true
	default:
		return false
	}
}

func pathsOverlap(left, right string) bool {
	left = strings.ToLower(path.Clean(filepath.ToSlash(left)))
	right = strings.ToLower(path.Clean(filepath.ToSlash(right)))
	return left == right ||
		strings.HasPrefix(left, right+"/") ||
		strings.HasPrefix(right, left+"/")
}
