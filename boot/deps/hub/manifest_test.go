package hub

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolutionError_String_WithConstraint(t *testing.T) {
	e := ResolutionError{
		Org:        "wippyai",
		Name:       "stdlib",
		Constraint: ">=1.0.0",
		Message:    "no matching version found",
	}
	assert.Equal(t, "wippyai/stdlib@>=1.0.0: no matching version found", e.String())
}

func TestResolutionError_String_WithoutConstraint(t *testing.T) {
	e := ResolutionError{
		Org:     "wippyai",
		Name:    "stdlib",
		Message: "module not found",
	}
	assert.Equal(t, "wippyai/stdlib: module not found", e.String())
}
