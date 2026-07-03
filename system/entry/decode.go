// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// fieldInfo caches struct field information for efficient field assignment
type fieldInfo struct {
	idIndex      int
	metaIndex    int
	hasIDField   bool
	hasMetaField bool
}

var (
	fieldCache sync.Map // map[reflect.Type]*fieldInfo
	metaType   = reflect.TypeOf((*attrs.Bag)(nil)).Elem()
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

// DecodeEntryConfigFromContext decodes an entry using the transcoder attached to ctx.
func DecodeEntryConfigFromContext[T any](ctx context.Context, entry registry.Entry) (*T, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, ErrTranscoderMissing
	}
	return DecodeEntryConfig[T](ctx, dtt, entry)
}

// DecodeEntryConfig decodes a registry entry into a configuration struct,
// resolving ${env:...} placeholders and legacy *_env companion fields against
// the environment registry.
func DecodeEntryConfig[T any](ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (*T, error) {
	return decodeEntryConfig[T](ctx, dtt, entry, true)
}

// DecodeEntryConfigRaw decodes a registry entry without resolving placeholders
// or *_env fields. Boot-pipeline stages use it to decode structural entries
// (ns.requirement, ns.dependency, ns.definition) whose values are moved between
// entries verbatim; resolution happens later when a service decodes the target.
func DecodeEntryConfigRaw[T any](ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (*T, error) {
	return decodeEntryConfig[T](ctx, dtt, entry, false)
}

func decodeEntryConfig[T any](ctx context.Context, dtt payload.Transcoder, entry registry.Entry, resolve bool) (*T, error) {
	if entry.Data == nil {
		return nil, ErrConfigurationDataRequired
	}

	// Unmarshal the original payload when nothing was resolved so the fast path
	// stays identical to a decode without placeholder support.
	source := entry.Data
	if resolve {
		data, err := dataAsMap(dtt, entry.Data)
		if err != nil {
			return nil, err
		}
		resolved, changed, err := resolveDataMap(ctx, "", "", skipPathsForType(reflect.TypeFor[T]()), data)
		if err != nil {
			return nil, err
		}
		if changed {
			source = payload.New(resolved)
		}
	}

	cfg := new(T)
	if err := dtt.Unmarshal(source, cfg); err != nil {
		return nil, NewUnmarshalConfigError(err)
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

	// Initialize defaults if the config implements InitDefaults
	if initer, ok := any(cfg).(interface{ InitDefaults() }); ok {
		initer.InitDefaults()
	}

	// Resolve legacy *_env companion fields against the environment registry.
	if resolve {
		if err := resolveEnvFields(ctx, cfg); err != nil {
			return nil, err
		}
	}

	// Validate if the config implements Validate
	if validator, ok := any(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, NewInvalidConfigurationError(err)
		}
	}

	return cfg, nil
}

// resolveEnvFields walks the config struct and overwrites every field whose json
// tag is "foo" with the value of an environment variable named by a sibling
// "foo_env" field. The walk recurses into nested structs, non-nil struct
// pointers, and embedded fields.
func resolveEnvFields(ctx context.Context, cfg any) error {
	v := reflect.ValueOf(cfg)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	var logged bool
	return walkEnvStruct(ctx, v, &logged)
}

func walkEnvStruct(ctx context.Context, v reflect.Value, logged *bool) error {
	t := v.Type()

	// Index sibling fields by json name so a "foo_env" field can find "foo".
	tagIndex := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if name := jsonName(t.Field(i)); name != "" && name != "-" {
			tagIndex[name] = i
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		fv := v.Field(i)

		switch fv.Kind() {
		case reflect.Struct:
			if err := walkEnvStruct(ctx, fv, logged); err != nil {
				return err
			}
		case reflect.Pointer:
			if !fv.IsNil() && fv.Elem().Kind() == reflect.Struct {
				if err := walkEnvStruct(ctx, fv.Elem(), logged); err != nil {
					return err
				}
			}
		}

		if fv.Kind() != reflect.String {
			continue
		}
		name := jsonName(field)
		base, ok := strings.CutSuffix(name, "_env")
		if !ok || base == "" {
			continue
		}
		variable := fv.String()
		if variable == "" {
			continue
		}
		sibIdx, ok := tagIndex[base]
		if !ok {
			continue
		}
		sibling := v.Field(sibIdx)
		if !sibling.CanSet() {
			continue
		}
		if err := applyEnvField(ctx, variable, sibling, logged); err != nil {
			return err
		}
	}
	return nil
}

// applyEnvField resolves the named variable and overwrites the sibling field.
func applyEnvField(ctx context.Context, name string, sibling reflect.Value, logged *bool) error {
	if !*logged {
		logs.GetLogger(ctx).Debug("entry uses deprecated *_env field; migrate to ${env:NAME} placeholders",
			zap.String("variable", name))
		*logged = true
	}

	reg := env.GetRegistry(ctx)
	if reg == nil {
		return NewEnvFieldRegistryMissingError(name)
	}

	value, found, err := reg.Lookup(ctx, name)
	if err != nil {
		if errors.Is(err, env.ErrVariableNotFound) {
			return nil
		}
		return NewEnvFieldLookupError(name, err)
	}
	if !found || value == "" {
		return nil
	}

	return assignEnvValue(sibling, value, name)
}

// assignEnvValue converts a string to the sibling field's kind, honoring bit size.
func assignEnvValue(sibling reflect.Value, value, name string) error {
	switch sibling.Kind() {
	case reflect.String:
		sibling.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, sibling.Type().Bits())
		if err != nil {
			return NewEnvFieldConversionError(name, value, sibling.Kind().String(), err)
		}
		sibling.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, sibling.Type().Bits())
		if err != nil {
			return NewEnvFieldConversionError(name, value, sibling.Kind().String(), err)
		}
		sibling.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, sibling.Type().Bits())
		if err != nil {
			return NewEnvFieldConversionError(name, value, sibling.Kind().String(), err)
		}
		sibling.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return NewEnvFieldConversionError(name, value, sibling.Kind().String(), err)
		}
		sibling.SetBool(b)
	default:
		return NewEnvFieldConversionError(name, value, sibling.Kind().String(), nil)
	}
	return nil
}

// jsonName returns the field's json tag name without options, ignoring "-".
func jsonName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return ""
	}
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		tag = tag[:idx]
	}
	return tag
}

var skipPathCache sync.Map // map[reflect.Type]map[string]struct{}

// skipPathsForType returns the data paths a config type excludes from
// placeholder resolution via resolve:"-" field tags. Fields carrying opaque
// content such as embedded source code use the tag so their values are never
// interpolated.
func skipPathsForType(t reflect.Type) map[string]struct{} {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	if cached, ok := skipPathCache.Load(t); ok {
		return cached.(map[string]struct{})
	}

	paths := make(map[string]struct{})
	collectSkipPaths(t, "", paths, make(map[reflect.Type]bool))
	if len(paths) == 0 {
		paths = nil
	}
	skipPathCache.Store(t, paths)
	return paths
}

func collectSkipPaths(t reflect.Type, prefix string, out map[string]struct{}, visiting map[reflect.Type]bool) {
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := jsonName(field)
		if name == "-" {
			continue
		}

		path := prefix
		if !field.Anonymous || name != "" {
			if name == "" {
				name = field.Name
			}
			path = joinPath(prefix, name)
		}

		if field.Tag.Get("resolve") == "-" {
			out[path] = struct{}{}
			continue
		}

		ft := field.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array || ft.Kind() == reflect.Map {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			collectSkipPaths(ft, path, out, visiting)
		}
	}
}
