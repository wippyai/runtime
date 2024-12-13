package loader

import (
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/internal/gostruct"
)

// Interpolator handles variable and file interpolation within payloads.
type Interpolator struct {
	variables      Variables
	fileReadFunc   func(string) (string, error) // Function to read file contents (optional)
	stringReplacer *gostruct.StringReplacer     // StringReplacer instance
}

// Variables is a map type for variable interpolation.
type Variables map[string]string

// NewInterpolator creates a new Interpolator.
// fileReadFunc is optional. If nil, file interpolation will be disabled.
func NewInterpolator(variables Variables, fileReadFunc func(string) (string, error)) *Interpolator {
	i := &Interpolator{
		variables:    variables,
		fileReadFunc: fileReadFunc,
	}

	// Create a StringReplacer only if needed
	if i.variables != nil || i.fileReadFunc != nil {
		i.stringReplacer = gostruct.NewStringReplacer(i.replaceString)
	}

	return i
}

// Interpolate performs variable and file interpolation on the payload data.
func (i *Interpolator) Interpolate(p payload.Payload, dtt payload.Transcoder) (payload.Payload, error) {
	if i.stringReplacer == nil {
		return p, nil // Nothing to do
	}

	// Unmarshal the payload data into a generic interface{}
	var data interface{}
	err := dtt.Unmarshal(p, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Perform replacements using the StringReplacer
	newData, err := i.stringReplacer.Replace(data)
	if err != nil {
		return nil, fmt.Errorf("failed to interpolate: %w", err)
	}

	// Create a new payload with the modified data and the original format
	return payload.NewPayload(newData, p.Format()), nil
}

// replaceString is the string replacement function used by StringReplacer.
// It handles both file:// and ${var} replacements.
func (i *Interpolator) replaceString(s string) (string, error) {
	// Handle file:// prefix
	if strings.HasPrefix(s, "file://") {
		if i.fileReadFunc == nil {
			return "", fmt.Errorf("file interpolation not enabled")
		}
		return i.fileReadFunc(strings.TrimPrefix(s, "file://"))
	}

	// Handle ${var} if variables enabled
	if i.variables != nil {
		return i.replaceVariables(s), nil
	}

	return s, nil
}

// replaceVariables handles ${var} replacements.
func (i *Interpolator) replaceVariables(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}

	result := s
	for k, v := range i.variables {
		placeholder := "${" + k + "}"
		result = strings.ReplaceAll(result, placeholder, v)
	}

	return result
}
