package interpolate

import (
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/stretchr/testify/assert"
)

// MockTranscoder implements payload.Transcoder interface for testing
type MockTranscoder struct{}

func (m *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	if data, ok := p.Data().(map[string]interface{}); ok {
		*(v.(*interface{})) = data
	} else {
		*(v.(*interface{})) = p.Data()
	}
	return nil
}

func TestNewEntryInterpolator(t *testing.T) {
	dtt := &MockTranscoder{}

	t.Run("creates helper with no interpolators", func(t *testing.T) {
		h := NewEntryInterpolator(dtt)
		assert.NotNil(t, h)
		assert.Empty(t, h.interpolators)
	})

	t.Run("creates helper with interpolators", func(t *testing.T) {
		mockInterpolator := func(s string, ctx interface{}) (string, error) {
			return s, nil
		}
		h := NewEntryInterpolator(dtt, WithInterpolator(mockInterpolator))
		assert.NotNil(t, h)
		assert.Len(t, h.interpolators, 1)
	})
}

func TestHelper_Interpolate(t *testing.T) {
	dtt := &MockTranscoder{}
	ctx := EntryContext{
		Vars:     Variables{"ENV": "production"},
		RootDir:  "/root",
		Filename: "config.yaml",
	}

	tests := []struct {
		name    string
		input   payload.Payload
		want    interface{}
		wantErr bool
		setupFn func(*Helper)
	}{
		{
			name:  "interpolate string",
			input: payload.New("Hello ${ENV}"),
			want:  "Hello production",
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators, LoadVars)
			},
		},
		{
			name: "interpolate map",
			input: payload.New(map[string]interface{}{
				"greeting": "Hello ${ENV}",
				"port":     8080,
			}),
			want: map[string]interface{}{
				"greeting": "Hello production",
				"port":     8080,
			},
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators, LoadVars)
			},
		},
		{
			name: "interpolate slice",
			input: payload.New([]interface{}{
				"Hello ${ENV}",
				123,
				"World",
			}),
			want: []interface{}{
				"Hello production",
				123,
				"World",
			},
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators, LoadVars)
			},
		},
		{
			name:  "multiple interpolators",
			input: payload.New("file://${ENV}/config.yaml"),
			want:  "Hello from production!",
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators,
					LoadVars,
					func(s string, ctx interface{}) (string, error) {
						if s == "file://production/config.yaml" {
							return "Hello from production!", nil
						}
						return s, nil
					},
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEntryInterpolator(dtt)
			if tt.setupFn != nil {
				tt.setupFn(h)
			}

			got, err := h.Interpolate(tt.input, ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got.Data())
		})
	}
}

func TestHelper_interpolateString(t *testing.T) {
	dtt := &MockTranscoder{}
	ctx := EntryContext{
		Vars:     Variables{"KEY": "value"},
		RootDir:  "/root",
		Filename: "config.yaml",
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
		setupFn func(*Helper)
	}{
		{
			name:  "single interpolator",
			input: "Hello ${KEY}",
			want:  "Hello value",
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators, LoadVars)
			},
		},
		{
			name:  "multiple interpolators",
			input: "Hello ${KEY}",
			want:  "HELLO VALUE",
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators,
					LoadVars,
					func(s string, _ interface{}) (string, error) {
						return strings.ToUpper(s), nil
					},
				)
			},
		},
		{
			name:  "no interpolators",
			input: "Hello ${KEY}",
			want:  "Hello ${KEY}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEntryInterpolator(dtt)
			if tt.setupFn != nil {
				tt.setupFn(h)
			}

			got, err := h.interpolateString(tt.input, ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHelper_interpolateMap(t *testing.T) {
	dtt := &MockTranscoder{}
	ctx := EntryContext{
		Vars:     Variables{"KEY": "value"},
		RootDir:  "/root",
		Filename: "config.yaml",
	}

	tests := []struct {
		name    string
		input   map[string]interface{}
		want    map[string]interface{}
		wantErr bool
		setupFn func(*Helper)
	}{
		{
			name: "nested map interpolation",
			input: map[string]interface{}{
				"string": "Hello ${KEY}",
				"number": 42,
				"nested": map[string]interface{}{
					"key": "${KEY}",
				},
			},
			want: map[string]interface{}{
				"string": "Hello value",
				"number": 42,
				"nested": map[string]interface{}{
					"key": "value",
				},
			},
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators, LoadVars)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEntryInterpolator(dtt)
			if tt.setupFn != nil {
				tt.setupFn(h)
			}

			got, err := h.interpolateMap(tt.input, ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHelper_interpolateSlice(t *testing.T) {
	dtt := &MockTranscoder{}
	ctx := EntryContext{
		Vars:     Variables{"KEY": "value"},
		RootDir:  "/root",
		Filename: "config.yaml",
	}

	tests := []struct {
		name    string
		input   []interface{}
		want    []interface{}
		wantErr bool
		setupFn func(*Helper)
	}{
		{
			name: "mixed slice interpolation",
			input: []interface{}{
				"Hello ${KEY}",
				42,
				map[string]interface{}{
					"key": "${KEY}",
				},
			},
			want: []interface{}{
				"Hello value",
				42,
				map[string]interface{}{
					"key": "value",
				},
			},
			setupFn: func(h *Helper) {
				h.interpolators = append(h.interpolators, LoadVars)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEntryInterpolator(dtt)
			if tt.setupFn != nil {
				tt.setupFn(h)
			}

			got, err := h.interpolateSlice(tt.input, ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
