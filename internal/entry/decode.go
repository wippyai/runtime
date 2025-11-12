package entry

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// fieldInfo caches struct field information for efficient field assignment
type fieldInfo struct {
	hasIDField   bool
	idIndex      int
	hasMetaField bool
	metaIndex    int
}

var (
	fieldCache sync.Map // map[reflect.Type]*fieldInfo
	metaType   = reflect.TypeOf((*registry.Metadata)(nil)).Elem()
	idType     = reflect.TypeOf(registry.ID{})
)

// getFieldInfo returns cached field information for a type, computing it if necessary
func getFieldInfo(t reflect.Type) *fieldInfo {
	if cached, ok := fieldCache.Load(t); ok {
		return cached.(*fieldInfo)
	}

	info := &fieldInfo{}

	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)

			if !field.IsExported() {
				continue
			}

			if field.Name == "ID" && field.Type == idType {
				info.hasIDField = true
				info.idIndex = i
			}

			if field.Name == "Meta" && field.Type == metaType {
				info.hasMetaField = true
				info.metaIndex = i
			}
		}
	}

	fieldCache.Store(t, info)
	return info
}

// DecodeEntryConfig decodes a registry entry into a configuration struct
func DecodeEntryConfig[T any](ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (*T, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("configuration data is required")
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Use reflection to automatically set ID and Meta fields if they exist
	v := reflect.ValueOf(cfg).Elem()
	if v.Kind() == reflect.Struct {
		info := getFieldInfo(v.Type())

		// Set ID field if present and entry has ID
		if info.hasIDField {
			idField := v.Field(info.idIndex)
			if idField.CanSet() && idField.IsZero() {
				idField.Set(reflect.ValueOf(entry.ID))
			}
		}

		// Set Meta field if present and entry has Meta
		if info.hasMetaField && entry.Meta != nil {
			metaField := v.Field(info.metaIndex)
			if metaField.CanSet() && metaField.IsNil() {
				metaField.Set(reflect.ValueOf(entry.Meta))
			}
		}
	}

	// Validate if the config implements Validate
	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return cfg, nil
}
