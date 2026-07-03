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
	var envData map[string]any
	if resolve {
		data, err := dataAsMap(dtt, entry.Data)
		if err != nil {
			return nil, err
		}
		resolved, changed, err := resolveDataMap(ctx, "", "", skipPathsForType(reflect.TypeFor[T]()), data)
		if err != nil {
			return nil, err
		}
		envData = resolved
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

	// Resolve legacy *_env directives from the entry data against the
	// environment registry.
	if resolve {
		if err := resolveEnvFields(ctx, cfg, envData); err != nil {
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
func resolveEnvFields(ctx context.Context, cfg any, data map[string]any) error {
	// Without an env registry in context there is nothing to resolve against.
	// A service that supplies its registry another way (an injected dependency)
	// resolves its own directives, so nothing is applied here.
	if env.GetRegistry(ctx) == nil {
		return nil
	}

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
	return walkEnvStruct(ctx, v, data, &logged)
}

// walkEnvStruct applies "<field>_env" directives read from the raw entry data
// onto the decoded struct. The directive names an environment variable; its
// resolved value is coerced to the target field's type and assigned. Because the
// directive is read from the data map rather than a companion struct field, the
// config type needs no "*Env" field. Nested structs and the data recurse
// together so a directive like tls.cert_env reaches the nested field.
func walkEnvStruct(ctx context.Context, v reflect.Value, data map[string]any, logged *bool) error {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := jsonName(field)
		if name == "-" {
			continue
		}
		fv := v.Field(i)

		switch fv.Kind() {
		case reflect.Struct:
			if err := walkEnvStruct(ctx, fv, childMap(data, name), logged); err != nil {
				return err
			}
		case reflect.Pointer:
			if !fv.IsNil() && fv.Elem().Kind() == reflect.Struct {
				if err := walkEnvStruct(ctx, fv.Elem(), childMap(data, name), logged); err != nil {
					return err
				}
			}
		case reflect.Map:
			// The attrs.Bag meta field uses the *_env convention for dependency
			// references, not value resolution, so it is left untouched.
			if field.Type != metaType {
				if err := resolveEnvMap(ctx, fv, logged); err != nil {
					return err
				}
			}
		}

		if name == "" || !fv.CanSet() {
			continue
		}
		variable := dataEnvDirective(data, name)
		if variable == "" {
			continue
		}
		value, overwrite, err := lookupEnvField(ctx, variable, logged)
		if err != nil {
			return err
		}
		if !overwrite {
			continue
		}
		if err := assignEnvValue(fv, value, variable); err != nil {
			return err
		}
	}
	return nil
}

// childMap returns the nested data submap under name, or nil when absent.
func childMap(data map[string]any, name string) map[string]any {
	if data == nil {
		return nil
	}
	if m, ok := data[name].(map[string]any); ok {
		return m
	}
	return nil
}

// dataEnvDirective returns the variable named by a "<base>_env" key in data.
func dataEnvDirective(data map[string]any, base string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[base+"_env"].(string); ok {
		return v
	}
	return ""
}

// resolveEnvMap resolves "foo_env" keys inside a string-keyed map whose values
// are strings (map[string]string) or arbitrary (map[string]any). Each directive
// key populates its base "foo" key with the resolved variable and is removed so
// it never leaks as a real option. Nested map[string]any values recurse, so a
// meta options bag is covered.
func resolveEnvMap(ctx context.Context, m reflect.Value, logged *bool) error {
	if m.IsNil() || m.Type().Key().Kind() != reflect.String {
		return nil
	}
	elemKind := m.Type().Elem().Kind()
	if elemKind != reflect.String && elemKind != reflect.Interface {
		return nil
	}

	for _, key := range m.MapKeys() {
		val := m.MapIndex(key)

		// Recurse into nested bags carried as map[string]any values.
		if elemKind == reflect.Interface {
			if nested, ok := val.Interface().(map[string]any); ok {
				if err := resolveEnvMap(ctx, reflect.ValueOf(nested), logged); err != nil {
					return err
				}
				continue
			}
		}

		base, ok := strings.CutSuffix(key.String(), "_env")
		if !ok || base == "" {
			continue
		}
		variable := envMapString(val)

		// Drop the directive key regardless of outcome.
		m.SetMapIndex(key, reflect.Value{})
		if variable == "" {
			continue
		}

		value, overwrite, err := lookupEnvField(ctx, variable, logged)
		if err != nil {
			return err
		}
		if !overwrite {
			continue
		}
		m.SetMapIndex(reflect.ValueOf(base).Convert(m.Type().Key()), reflect.ValueOf(value).Convert(m.Type().Elem()))
	}
	return nil
}

// envMapString extracts a string from a map value that is a string or an
// interface wrapping a string; anything else yields "".
func envMapString(v reflect.Value) string {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if v.Kind() == reflect.String {
		return v.String()
	}
	return ""
}

// lookupEnvField resolves a legacy *_env variable against the registry in ctx.
// overwrite reports whether the caller should apply value: a registered-but-empty
// variable keeps the existing value (overwrite=false) while an absent or
// unresolvable variable is an error.
func lookupEnvField(ctx context.Context, name string, logged *bool) (value string, overwrite bool, err error) {
	if !*logged {
		logs.GetLogger(ctx).Debug("entry uses deprecated *_env field; migrate to ${env:NAME} placeholders",
			zap.String("variable", name))
		*logged = true
	}

	reg := env.GetRegistry(ctx)
	if reg == nil {
		return "", false, NewEnvFieldRegistryMissingError(name)
	}

	// Get honors a variable's DefaultValue when its storage holds no value,
	// matching the per-service resolvers this pass replaces.
	value, err = reg.Get(ctx, name)
	if err != nil {
		if errors.Is(err, env.ErrVariableNotFound) {
			return "", false, NewEnvFieldNotFoundError(name)
		}
		return "", false, NewEnvFieldLookupError(name, err)
	}
	// A registered-but-empty variable keeps the inline value.
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
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
