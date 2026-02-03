package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentinelErrors(t *testing.T) {
	assert.Equal(t, "collector is closed", ErrClosed.Error())
	assert.Equal(t, "exporter already registered", ErrExporterExists.Error())
	assert.Equal(t, "invalid exporter", ErrInvalidExporter.Error())
}
