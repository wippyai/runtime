package protocol

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    lua.LValue
		expected time.Duration
		wantErr  bool
	}{
		// Number inputs (milliseconds)
		{
			name:     "positive number",
			input:    lua.LNumber(1000),
			expected: time.Second,
		},
		{
			name:     "zero number",
			input:    lua.LNumber(0),
			expected: 0,
		},
		{
			name:     "negative number",
			input:    lua.LNumber(-1000),
			expected: -time.Second,
		},
		{
			name:     "decimal number",
			input:    lua.LNumber(1500.5),
			expected: 1500 * time.Millisecond,
		},

		// String inputs - pure numeric
		{
			name:     "numeric string",
			input:    lua.LString("1000"),
			expected: time.Second,
		},
		{
			name:     "zero string",
			input:    lua.LString("0"),
			expected: 0,
		},

		// String inputs - with units
		{
			name:     "milliseconds",
			input:    lua.LString("1000ms"),
			expected: time.Second,
		},
		{
			name:     "seconds",
			input:    lua.LString("1s"),
			expected: time.Second,
		},
		{
			name:     "minutes",
			input:    lua.LString("2m"),
			expected: 2 * time.Minute,
		},
		{
			name:     "hours",
			input:    lua.LString("1h"),
			expected: time.Hour,
		},
		{
			name:     "complex duration",
			input:    lua.LString("1h30m45s"),
			expected: time.Hour + 30*time.Minute + 45*time.Second,
		},

		// String inputs - with spaces
		{
			name:     "space between number and unit",
			input:    lua.LString("1 s"),
			expected: time.Second,
		},
		{
			name:     "multiple spaces",
			input:    lua.LString("  1  s  "),
			expected: time.Second,
		},

		// Invalid inputs
		{
			name:    "invalid string format",
			input:   lua.LString("invalid"),
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   lua.LString(""),
			wantErr: true,
		},
		{
			name:    "invalid unit",
			input:   lua.LString("1x"),
			wantErr: true,
		},
		{
			name:    "boolean value",
			input:   lua.LBool(true),
			wantErr: true,
		},
		{
			name:    "nil value",
			input:   lua.LNil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestCheckDuration(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		name        string
		setupStack  func()
		index       int
		expected    time.Duration
		shouldPanic bool
	}{
		{
			name: "valid number at top",
			setupStack: func() {
				l.Push(lua.LNumber(1000))
			},
			index:    1,
			expected: time.Second,
		},
		{
			name: "valid string at position",
			setupStack: func() {
				l.Push(lua.LString("dummy"))
				l.Push(lua.LString("2s"))
				l.Push(lua.LString("dummy"))
			},
			index:    2,
			expected: 2 * time.Second,
		},
		{
			name: "invalid type should panic",
			setupStack: func() {
				l.Push(lua.LBool(true))
			},
			index:       1,
			shouldPanic: true,
		},
		{
			name: "invalid index should panic",
			setupStack: func() {
				l.Push(lua.LNumber(1000))
			},
			index:       999,
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear stack before each test
			l.SetTop(0)

			// Setup test state
			tt.setupStack()

			if tt.shouldPanic {
				assert.Panics(t, func() {
					CheckDuration(l, tt.index)
				})
			} else {
				duration := CheckDuration(l, tt.index)
				assert.Equal(t, tt.expected, duration)
			}
		})
	}
}

func TestPushDuration(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		name     string
		duration time.Duration
		expected float64
	}{
		{
			name:     "one second",
			duration: time.Second,
			expected: 1000,
		},
		{
			name:     "zero duration",
			duration: 0,
			expected: 0,
		},
		{
			name:     "negative duration",
			duration: -500 * time.Millisecond,
			expected: -500,
		},
		{
			name:     "complex duration",
			duration: 2*time.Hour + 30*time.Minute + 45*time.Second,
			expected: (2*3600 + 30*60 + 45) * 1000,
		},
		{
			name:     "sub-millisecond",
			duration: 500 * time.Microsecond,
			expected: 0, // d.Milliseconds() truncates sub-millisecond values
		},
		{
			name:     "just over millisecond",
			duration: time.Millisecond + 500*time.Microsecond,
			expected: 1, // d.Milliseconds() truncates to 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear stack before each test
			l.SetTop(0)

			// Push duration and verify
			PushDuration(l, tt.duration)

			require.Equal(t, 1, l.GetTop(), "stack should have exactly one value")

			value := l.Get(-1)
			number, ok := value.(lua.LNumber)
			require.True(t, ok, "pushed value should be a number")

			assert.Equal(t, tt.expected, float64(number))
		})
	}
}
