package loader

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// ValidationError represents a validation error with context
type ValidationError struct {
	Field   string
	Message string
	Index   int // For array validation errors
}

func (e ValidationError) Error() string {
	if e.Index >= 0 {
		return fmt.Sprintf("entry[%d].%s: %s", e.Index, e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// SkipFileError indicates file should be skipped silently
type SkipFileError struct {
	Reason string
}

func (e SkipFileError) Error() string {
	return fmt.Sprintf("skip file: %s", e.Reason)
}

// ProcessingError represents an error during entry processing
type ProcessingError struct {
	Operation string
	EntryID   string
	Err       error
}

func (e ProcessingError) Error() string {
	return fmt.Sprintf("processing error in %s for entry %s: %v", e.Operation, e.EntryID, e.Err)
}

func (e ProcessingError) Unwrap() error {
	return e.Err
}

// Export represents a capability that a module/system makes available to dependent modules
type Export struct {
	Name    string            `json:"name" yaml:"name"`
	Targets map[string]string `json:"targets,omitempty" yaml:"targets,omitempty"`
}

// FileContent represents the structure of a registry configuration file.
// It supports both single entry and batch entries formats, with common
// metadata that can be applied to all entries in a file.
type FileContent struct {
	Version   string            `json:"version,omitempty" yaml:"version,omitempty"`
	Namespace string            `json:"namespace"`
	Meta      registry.Metadata `json:"meta,omitempty" yaml:"meta,omitempty"`

	// Store raw entries as map slice
	RawEntries []map[string]interface{} `json:"entries,omitempty" yaml:"entries,omitempty"`

	// Single-entry format fields
	Name string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Kind string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Data map[string]interface{} `json:",inline"`
}

// EntryProcessor handles the processing of registry entries
type EntryProcessor struct {
	transcoder payload.Transcoder
	validator  *EntryValidator
}

// NewEntryProcessor creates a new entry processor with the given transcoder
func NewEntryProcessor(transcoder payload.Transcoder) *EntryProcessor {
	return &EntryProcessor{
		transcoder: transcoder,
		validator:  NewEntryValidator(),
	}
}

// ExtractDependenciesToEntries extracts and processes dependencies to registry entries
func (ep *EntryProcessor) ExtractDependenciesToEntries(ctx context.Context, p payload.Payload) ([]registry.Entry, error) {
	content, err := ep.unmarshalContent(p)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal content: %w", err)
	}

	if err := ep.validator.ValidateFileContent(content); err != nil {
		// Skip files silently if they don't have required headers
		var skipErr SkipFileError
		if errors.As(err, &skipErr) {
			return []registry.Entry{}, nil
		}
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	entries := make([]registry.Entry, 0)

	// Process batch entries
	batchEntries, err := ep.processBatchEntries(ctx, content)
	if err != nil {
		return nil, err
	}
	entries = append(entries, batchEntries...)

	// Process single entry if applicable
	singleEntry, err := ep.processSingleEntry(ctx, content)
	if err != nil {
		return nil, err
	}
	if singleEntry != nil {
		entries = append(entries, *singleEntry)
	}

	return entries, nil
}

// unmarshalContent unmarshals the payload into FileContent
func (ep *EntryProcessor) unmarshalContent(p payload.Payload) (*FileContent, error) {
	var content FileContent
	if err := ep.transcoder.Unmarshal(p, &content); err != nil {
		return nil, fmt.Errorf("unmarshal content: %w", err)
	}
	return &content, nil
}

// processBatchEntries processes the entries array from the file content
func (ep *EntryProcessor) processBatchEntries(ctx context.Context, content *FileContent) ([]registry.Entry, error) {
	entries := make([]registry.Entry, 0, len(content.RawEntries))

	for i, rawEntry := range content.RawEntries {
		entry, err := ep.processRawEntry(ctx, content, rawEntry, i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// processRawEntry processes a single raw entry from the batch
func (ep *EntryProcessor) processRawEntry(_ context.Context, content *FileContent, rawEntry map[string]interface{}, index int) (registry.Entry, error) {
	// Validate required fields
	if err := ep.validator.ValidateRawEntry(rawEntry, index); err != nil {
		return registry.Entry{}, err
	}

	name := rawEntry["name"].(string)
	kind := rawEntry["kind"].(string)

	// Extract and merge metadata
	entryMeta := ep.extractMetadata(rawEntry)
	mergedMeta := ep.mergeMetadata(content.Meta, entryMeta)

	// Update the raw entry's meta field with merged metadata
	rawEntry["meta"] = mergedMeta

	// Create entry payload
	entryData := payload.New(rawEntry)

	entry := registry.Entry{
		ID:   registry.NewID(content.Namespace, name),
		Kind: kind,
		Meta: mergedMeta,
		Data: entryData,
	}

	return entry, nil
}

// processSingleEntry processes a single entry format if applicable
func (ep *EntryProcessor) processSingleEntry(_ context.Context, content *FileContent) (*registry.Entry, error) {
	// Only process if no batch entries and single entry fields are present
	if len(content.RawEntries) > 0 || content.Name == "" || content.Kind == "" {
		return nil, nil
	}

	// Validate single entry
	if err := ep.validator.ValidateSingleEntry(content); err != nil {
		return nil, err
	}

	// Merge metadata
	mergedMeta := ep.mergeMetadata(content.Meta, nil)

	// Build entry data preserving the original structure
	entryMap := ep.buildSingleEntryMap(content)

	entry := registry.Entry{
		ID:   registry.NewID(content.Namespace, content.Name),
		Kind: content.Kind,
		Meta: mergedMeta,
		Data: payload.New(entryMap),
	}

	return &entry, nil
}

// extractMetadata extracts metadata from a raw entry
func (ep *EntryProcessor) extractMetadata(rawEntry map[string]interface{}) registry.Metadata {
	if metaRaw, ok := rawEntry["meta"]; ok && metaRaw != nil {
		if metaMap, ok := metaRaw.(map[string]any); ok {
			return metaMap
		}
	}
	return nil
}

// buildSingleEntryMap builds the entry map for single entry format
func (ep *EntryProcessor) buildSingleEntryMap(content *FileContent) map[string]interface{} {
	entryMap := map[string]interface{}{
		"namespace": content.Namespace,
		"name":      content.Name,
		"kind":      content.Kind,
	}

	if content.Data != nil {
		// Create a nested data field with all the custom fields
		nestedData := ep.extractCustomFields(content.Data)
		if len(nestedData) > 0 {
			entryMap["data"] = nestedData
		}
	}

	return entryMap
}

// extractCustomFields extracts custom fields from the data, excluding structural fields
func (ep *EntryProcessor) extractCustomFields(data map[string]interface{}) map[string]interface{} {
	excludedFields := map[string]bool{
		"namespace":    true,
		"name":         true,
		"kind":         true,
		"meta":         true,
		"version":      true,
		"requirements": true,
		"entries":      true,
	}

	nestedData := make(map[string]interface{})
	for k, v := range data {
		if !excludedFields[k] {
			nestedData[k] = v
		}
	}

	return nestedData
}

// mergeMetadata merges base and override metadata with proper handling of different types
func (ep *EntryProcessor) mergeMetadata(baseMeta, overrideMeta registry.Metadata) registry.Metadata {
	if baseMeta == nil {
		return overrideMeta
	}
	if overrideMeta == nil {
		return baseMeta
	}

	merged := make(registry.Metadata)

	// Copy base metadata
	for k, v := range baseMeta {
		merged[k] = v
	}

	// Override with override metadata
	for k, v := range overrideMeta {
		merged[k] = v
	}

	return merged
}

// EntryValidator handles validation of registry entries
type EntryValidator struct{}

// NewEntryValidator creates a new entry validator
func NewEntryValidator() *EntryValidator {
	return &EntryValidator{}
}

// ValidateFileContent validates the overall file content structure
func (ev *EntryValidator) ValidateFileContent(content *FileContent) error {
	if content == nil {
		return &ValidationError{Field: "content", Message: "content cannot be nil"}
	}

	// If no namespace, skip file silently (not a wippy entry file)
	if strings.TrimSpace(content.Namespace) == "" {
		return SkipFileError{Reason: "no namespace header"}
	}

	// Validate that we have either batch entries or single entry format
	// Empty entries array is valid (returns empty result)
	// Only fail if we have no entries array AND no single entry fields
	// Note: An empty entries array (RawEntries = []) is considered valid
	if content.RawEntries == nil && content.Name == "" && content.Kind == "" {
		return &ValidationError{Field: "entries", Message: "either entries array or single entry (name/kind) must be provided"}
	}

	return nil
}

// ValidateRawEntry validates a single raw entry
func (ev *EntryValidator) ValidateRawEntry(rawEntry map[string]interface{}, index int) error {
	if rawEntry == nil {
		return &ValidationError{Field: "entry", Message: "entry cannot be nil", Index: index}
	}

	name, ok := rawEntry["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return &ValidationError{Field: "name", Message: "name is required and must be a non-empty string", Index: index}
	}

	kind, ok := rawEntry["kind"].(string)
	if !ok || strings.TrimSpace(kind) == "" {
		return &ValidationError{Field: "kind", Message: "kind is required and must be a non-empty string", Index: index}
	}

	return nil
}

// ValidateSingleEntry validates a single entry format
func (ev *EntryValidator) ValidateSingleEntry(content *FileContent) error {
	if strings.TrimSpace(content.Name) == "" {
		return &ValidationError{Field: "name", Message: "name is required for single entry format"}
	}

	if strings.TrimSpace(content.Kind) == "" {
		return &ValidationError{Field: "kind", Message: "kind is required for single entry format"}
	}

	return nil
}

// Legacy function for backward compatibility
func ExtractDependenciesToEntries(p payload.Payload, dtt payload.Transcoder) ([]registry.Entry, error) {
	processor := NewEntryProcessor(dtt)
	return processor.ExtractDependenciesToEntries(ctxapi.NewRootContext(), p)
}

// mergeMeta merges base and override metadata (legacy function for backward compatibility)
func mergeMeta(baseMeta, overrideMeta registry.Metadata) registry.Metadata {
	processor := NewEntryProcessor(nil)
	return processor.mergeMetadata(baseMeta, overrideMeta)
}
