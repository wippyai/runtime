package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xeipuuv/gojsonschema"
	"strings"
)

// ValidationError represents a JSON schema validation error
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %s", strings.Join(e.Errors, "; "))
}

// IsValidationError checks if an error is a ValidationError
func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return err != nil && errors.As(err, &validationErr)
}

// jsonValidator handles JSON schema validation
type jsonValidator struct {
	schema *gojsonschema.Schema
}

// newJsonValidator creates a validator from either string or object schema
func newJsonValidator(schema any) (*jsonValidator, error) {
	var loader gojsonschema.JSONLoader

	switch s := schema.(type) {
	case string:
		loader = gojsonschema.NewStringLoader(s)
	case []byte:
		loader = gojsonschema.NewBytesLoader(s)
	default:
		// For objects, marshal to JSON first
		schemaBytes, err := json.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("marshaling schema: %w", err)
		}
		loader = gojsonschema.NewBytesLoader(schemaBytes)
	}

	compiledSchema, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}

	return &jsonValidator{
		schema: compiledSchema,
	}, nil
}

// Validate validates JSON data against the schema
func (v *jsonValidator) Validate(data []byte) error {
	document := gojsonschema.NewBytesLoader(data)
	result, err := v.schema.Validate(document)
	if err != nil {
		return fmt.Errorf("validating: %w", err)
	}

	if !result.Valid() {
		var errors []string
		for _, err := range result.Errors() {
			errors = append(errors, err.String())
		}
		return &ValidationError{Errors: errors}
	}

	return nil
}
