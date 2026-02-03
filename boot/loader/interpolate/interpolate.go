package interpolate

import (
	"context"
	"io/fs"

	"github.com/wippyai/runtime/api/payload"
)

// EntryContext holds the context for loading and interpolating configuration entries.
// It contains the current configuration filename being processed, and a context for accessing services.
type EntryContext struct {
	FS       fs.FS
	Context  context.Context
	Filename string
}

// InterpolatorFunc defines the signature for interpolation functions
type InterpolatorFunc func(string, interface{}) (string, error)

// Helper provides a convenient way to manage payload interpolation
type Helper struct {
	dtt           payload.Transcoder
	interpolators []InterpolatorFunc
}

// Option defines a configuration option for the Helper
type Option func(*Helper)

// WithInterpolator adds an interpolator function to the Helper
func WithInterpolator(fn InterpolatorFunc) Option {
	return func(h *Helper) {
		h.interpolators = append(h.interpolators, fn)
	}
}

// NewEntryInterpolator creates a new Helper instance with the provided options
func NewEntryInterpolator(dtt payload.Transcoder, opts ...Option) *Helper {
	h := &Helper{
		dtt:           dtt,
		interpolators: make([]InterpolatorFunc, 0),
	}

	// Apply all options
	for _, opt := range opts {
		opt(h)
	}

	return h
}

// Interpolate processes a payload through all registered interpolators
func (h *Helper) Interpolate(p payload.Payload, ctx EntryContext) (payload.Payload, error) {
	var data interface{}
	if err := h.dtt.Unmarshal(p, &data); err != nil {
		return p, NewUnmarshalPayloadError(err)
	}

	interpolated, err := h.interpolateValue(data, ctx)
	if err != nil {
		return p, err
	}

	return payload.New(interpolated), nil
}

// interpolateValue recursively processes values through all interpolators
func (h *Helper) interpolateValue(v interface{}, ctx EntryContext) (interface{}, error) {
	switch val := v.(type) {
	case string:
		return h.interpolateString(val, ctx)
	case map[string]interface{}:
		return h.interpolateMap(val, ctx)
	case []interface{}:
		return h.interpolateSlice(val, ctx)
	default:
		return v, nil
	}
}

func (h *Helper) interpolateString(s string, ctx EntryContext) (string, error) {
	result := s
	for _, interpolator := range h.interpolators {
		var err error
		result, err = interpolator(result, ctx)
		if err != nil {
			return "", NewInterpolationError(err)
		}
	}
	return result, nil
}

func (h *Helper) interpolateMap(m map[string]interface{}, ctx EntryContext) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for k, v := range m {
		interpolated, err := h.interpolateValue(v, ctx)
		if err != nil {
			return nil, err
		}
		result[k] = interpolated
	}
	return result, nil
}

func (h *Helper) interpolateSlice(s []interface{}, ctx EntryContext) ([]interface{}, error) {
	result := make([]interface{}, len(s))
	for i, v := range s {
		interpolated, err := h.interpolateValue(v, ctx)
		if err != nil {
			return nil, err
		}
		result[i] = interpolated
	}
	return result, nil
}
