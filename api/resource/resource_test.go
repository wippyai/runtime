package resource

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAccessMode_IsValid(t *testing.T) {
	tests := []struct {
		name string
		mode AccessMode
		want bool
	}{
		{
			name: "read only",
			mode: ReadOnly,
			want: true,
		},
		{
			name: "write only",
			mode: WriteOnly,
			want: true,
		},
		{
			name: "read write",
			mode: ReadWrite,
			want: true,
		},
		{
			name: "exclusive",
			mode: Exclusive,
			want: true,
		},
		{
			name: "exclusive with read",
			mode: Exclusive | ReadOnly,
			want: false,
		},
		{
			name: "exclusive with write",
			mode: Exclusive | WriteOnly,
			want: false,
		},
		{
			name: "exclusive with read write",
			mode: Exclusive | ReadWrite,
			want: false,
		},
		{
			name: "zero mode",
			mode: AccessMode(0),
			want: false,
		},
		{
			name: "high bit mode",
			mode: AccessMode(1 << 7),
			want: false,
		},
		{
			name: "all bits set",
			mode: AccessMode(0xFF),
			want: false,
		},
		{
			name: "multiple non-exclusive bits",
			mode: ReadOnly | WriteOnly | AccessMode(1<<3),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mode.IsValid()
			assert.Equal(t, tt.want, got, "AccessMode.IsValid() for mode %08b", tt.mode)

			// Additional check for bit values
			if got {
				// If mode is valid, verify its bits
				switch tt.mode {
				case ReadOnly:
					assert.Equal(t, AccessMode(1), tt.mode)
				case WriteOnly:
					assert.Equal(t, AccessMode(2), tt.mode)
				case ReadWrite:
					assert.Equal(t, AccessMode(3), tt.mode)
				case Exclusive:
					assert.Equal(t, AccessMode(4), tt.mode)
				}
			}
		})
	}
}

func TestAccessMode_BitValues(t *testing.T) {
	// Test that constants have expected bit values
	assert.Equal(t, AccessMode(1), ReadOnly, "ReadOnly should be bit 0")
	assert.Equal(t, AccessMode(2), WriteOnly, "WriteOnly should be bit 1")
	assert.Equal(t, AccessMode(3), ReadWrite, "ReadWrite should be ReadOnly|WriteOnly")
	assert.Equal(t, AccessMode(4), Exclusive, "Exclusive should be bit 2")

	// Test that ReadWrite is correctly defined as combination
	assert.Equal(t, ReadOnly|WriteOnly, ReadWrite, "ReadWrite should equal ReadOnly|WriteOnly")
}
