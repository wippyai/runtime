package entry

import (
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Mutator provides operations to modify registry entries
type Mutator struct {
	dtt payload.Transcoder
}

// NewMutator creates a new entry mutator
func NewMutator(dtt payload.Transcoder) *Mutator {
	return &Mutator{dtt: dtt}
}

// Set sets a value at the given path in the entry.
// Path can be "data.field.nested" or ".data.field.nested" (leading dot is trimmed).
// Supports paths starting with "data" or "meta".
func (m *Mutator) Set(entry *registry.Entry, path string, value any) error {
	target, segments, err := parsePath(path)
	if err != nil {
		return err
	}

	switch target {
	case "data":
		return m.setInData(entry, segments, value)
	case "meta":
		return m.setInMeta(entry, segments, value)
	default:
		return fmt.Errorf("invalid target: %s (must be 'data' or 'meta')", target)
	}
}

// Append appends values to an array at the given path, with automatic deduplication.
// If the path doesn't exist, creates a new array.
// If the existing value is not an array, returns an error.
func (m *Mutator) Append(entry *registry.Entry, path string, values ...any) error {
	target, segments, err := parsePath(path)
	if err != nil {
		return err
	}

	switch target {
	case "data":
		return m.appendInData(entry, segments, values...)
	case "meta":
		return m.appendInMeta(entry, segments, values...)
	default:
		return fmt.Errorf("invalid target: %s (must be 'data' or 'meta')", target)
	}
}

// Delete removes a field at the given path.
func (m *Mutator) Delete(entry *registry.Entry, path string) error {
	target, segments, err := parsePath(path)
	if err != nil {
		return err
	}

	switch target {
	case "data":
		return m.deleteInData(entry, segments)
	case "meta":
		return m.deleteInMeta(entry, segments)
	default:
		return fmt.Errorf("invalid target: %s (must be 'data' or 'meta')", target)
	}
}

// parsePath parses a path string into target and segments.
// Supports both "data.field" and ".data.field" formats.
// If path doesn't start with "data" or "meta", treats it as "data.*"
func parsePath(path string) (target string, segments []string, err error) {
	// Trim leading dot if present
	path = strings.TrimPrefix(path, ".")

	if path == "" {
		return "", nil, fmt.Errorf("empty path")
	}

	// Split by dot
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty path after split")
	}

	// Check if first part is explicit target
	target = parts[0]
	if target == "data" || target == "meta" {
		// Explicit target specified
		segments = parts[1:]
		return target, segments, nil
	}

	// No explicit target, treat entire path as data.*
	target = "data"
	segments = parts
	return target, segments, nil
}

// setInData sets a value in entry.Data at the given path segments
func (m *Mutator) setInData(entry *registry.Entry, segments []string, value any) error {
	data, err := m.ensureDataAsMap(entry)
	if err != nil {
		return err
	}

	if len(segments) == 0 {
		// Setting entire data is not supported for safety
		return fmt.Errorf("cannot replace entire data field")
	}

	if err := setValueAtPath(data, segments, value); err != nil {
		return err
	}

	entry.Data = payload.New(data)
	return nil
}

// setInMeta sets a value in entry.Meta at the given path segments
func (m *Mutator) setInMeta(entry *registry.Entry, segments []string, value any) error {
	if entry.Meta == nil {
		entry.Meta = make(registry.Metadata)
	}

	if len(segments) == 0 {
		return fmt.Errorf("cannot replace entire meta field")
	}

	// For single segment, set directly in Meta map
	if len(segments) == 1 {
		entry.Meta[segments[0]] = value
		return nil
	}

	// For nested segments, navigate through maps
	current := make(map[string]any)
	for k, v := range entry.Meta {
		current[k] = v
	}

	if err := setValueAtPath(current, segments, value); err != nil {
		return err
	}

	// Copy back to Meta
	for k, v := range current {
		entry.Meta[k] = v
	}

	return nil
}

// appendInData appends values to an array in entry.Data with deduplication
func (m *Mutator) appendInData(entry *registry.Entry, segments []string, values ...any) error {
	data, err := m.ensureDataAsMap(entry)
	if err != nil {
		return err
	}

	if len(segments) == 0 {
		return fmt.Errorf("cannot append to entire data field")
	}

	if err := appendToArrayAtPath(data, segments, values...); err != nil {
		return err
	}

	entry.Data = payload.New(data)
	return nil
}

// appendInMeta appends values to an array in entry.Meta with deduplication
func (m *Mutator) appendInMeta(entry *registry.Entry, segments []string, values ...any) error {
	if entry.Meta == nil {
		entry.Meta = make(registry.Metadata)
	}

	if len(segments) == 0 {
		return fmt.Errorf("cannot append to entire meta field")
	}

	// Convert Meta to map[string]any for manipulation
	current := make(map[string]any)
	for k, v := range entry.Meta {
		current[k] = v
	}

	if err := appendToArrayAtPath(current, segments, values...); err != nil {
		return err
	}

	// Copy back to Meta
	for k, v := range current {
		entry.Meta[k] = v
	}

	return nil
}

// deleteInData deletes a field from entry.Data
func (m *Mutator) deleteInData(entry *registry.Entry, segments []string) error {
	data, err := m.ensureDataAsMap(entry)
	if err != nil {
		return err
	}

	if len(segments) == 0 {
		return fmt.Errorf("cannot delete entire data field")
	}

	if err := deleteAtPath(data, segments); err != nil {
		return err
	}

	entry.Data = payload.New(data)
	return nil
}

// deleteInMeta deletes a field from entry.Meta
func (m *Mutator) deleteInMeta(entry *registry.Entry, segments []string) error {
	if entry.Meta == nil {
		return nil // Nothing to delete
	}

	if len(segments) == 0 {
		return fmt.Errorf("cannot delete entire meta field")
	}

	// For single segment, delete directly
	if len(segments) == 1 {
		delete(entry.Meta, segments[0])
		return nil
	}

	// For nested segments, navigate and delete
	current := make(map[string]any)
	for k, v := range entry.Meta {
		current[k] = v
	}

	if err := deleteAtPath(current, segments); err != nil {
		return err
	}

	// Copy back to Meta
	entry.Meta = make(registry.Metadata)
	for k, v := range current {
		entry.Meta[k] = v
	}

	return nil
}

// ensureDataAsMap ensures entry.Data is in map[string]any format
func (m *Mutator) ensureDataAsMap(entry *registry.Entry) (map[string]any, error) {
	if entry.Data == nil {
		return make(map[string]any), nil
	}

	// Check if already in Golang format
	if entry.Data.Format() == payload.Golang {
		if data, ok := entry.Data.Data().(map[string]any); ok {
			return data, nil
		}
		return make(map[string]any), nil
	}

	// Transcode to Golang format
	golangPayload, err := m.dtt.Transcode(entry.Data, payload.Golang)
	if err != nil {
		return nil, fmt.Errorf("failed to transcode to golang format: %w", err)
	}

	if data, ok := golangPayload.Data().(map[string]any); ok {
		return data, nil
	}

	return make(map[string]any), nil
}

// setValueAtPath sets a value at the given path in a nested map structure
func setValueAtPath(data map[string]any, segments []string, value any) error {
	if len(segments) == 0 {
		return fmt.Errorf("empty path segments")
	}

	current := data
	for i := 0; i < len(segments)-1; i++ {
		segment := segments[i]

		if next, ok := current[segment]; ok {
			if nextMap, ok := next.(map[string]any); ok {
				current = nextMap
			} else {
				// Path exists but isn't a map - overwrite with new map
				newMap := make(map[string]any)
				current[segment] = newMap
				current = newMap
			}
		} else {
			// Create missing intermediate maps
			newMap := make(map[string]any)
			current[segment] = newMap
			current = newMap
		}
	}

	current[segments[len(segments)-1]] = value
	return nil
}

// appendToArrayAtPath appends values to an array at the given path with deduplication
func appendToArrayAtPath(data map[string]any, segments []string, values ...any) error {
	if len(segments) == 0 {
		return fmt.Errorf("empty path segments")
	}

	// Navigate to parent
	current := data
	for i := 0; i < len(segments)-1; i++ {
		segment := segments[i]

		if next, ok := current[segment]; ok {
			if nextMap, ok := next.(map[string]any); ok {
				current = nextMap
			} else {
				// Path exists but isn't a map - overwrite
				newMap := make(map[string]any)
				current[segment] = newMap
				current = newMap
			}
		} else {
			// Create missing intermediate maps
			newMap := make(map[string]any)
			current[segment] = newMap
			current = newMap
		}
	}

	lastSegment := segments[len(segments)-1]

	// Get existing array or create new one
	var existing []any
	if existingVal, ok := current[lastSegment]; ok {
		if existingArray, ok := existingVal.([]any); ok {
			existing = existingArray
		} else if existingArray, ok := existingVal.([]interface{}); ok {
			existing = existingArray
		} else {
			return fmt.Errorf("cannot append to non-array field at path %s", strings.Join(segments, "."))
		}
	}

	// Build deduplication map
	seen := make(map[string]struct{})
	for _, v := range existing {
		key := fmt.Sprintf("%v", v)
		seen[key] = struct{}{}
	}

	// Append only unique values
	for _, v := range values {
		key := fmt.Sprintf("%v", v)
		if _, exists := seen[key]; !exists {
			existing = append(existing, v)
			seen[key] = struct{}{}
		}
	}

	current[lastSegment] = existing
	return nil
}

// deleteAtPath deletes a field at the given path
func deleteAtPath(data map[string]any, segments []string) error {
	if len(segments) == 0 {
		return fmt.Errorf("empty path segments")
	}

	// Navigate to parent
	current := data
	for i := 0; i < len(segments)-1; i++ {
		segment := segments[i]

		if next, ok := current[segment]; ok {
			if nextMap, ok := next.(map[string]any); ok {
				current = nextMap
			} else {
				// Path doesn't lead to the target, nothing to delete
				return nil
			}
		} else {
			// Path doesn't exist, nothing to delete
			return nil
		}
	}

	delete(current, segments[len(segments)-1])
	return nil
}
