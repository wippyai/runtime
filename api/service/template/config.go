// SPDX-License-Identifier: MPL-2.0

// Package template provides template service configuration.
package template

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// Registry kind constants for Template components
const (
	// Jet identifies a template component
	Jet registry.Kind = "template.jet"
	// Set identifies a template set component
	Set registry.Kind = "template.set"
)

// Config represents configuration for a template entry
type Config struct {
	Meta attrs.Bag `json:"meta"`

	// Source defines the template content or location
	Source string `json:"source"`

	// Set is the template set this template belongs to
	Set registry.ID `json:"set"`
}

// EngineConfig contains Jet template engine configuration
type EngineConfig struct {
	Globals         map[string]any  `json:"globals"`
	Delimiters      DelimiterConfig `json:"delimiters"`
	Extensions      []string        `json:"extensions"`
	DevelopmentMode bool            `json:"development_mode"`
}

// DelimiterConfig allows customizing template delimiters
type DelimiterConfig struct {
	// Left is the left delimiter (default: "{{")
	Left string `json:"left"`

	// Right is the right delimiter (default: "}}")
	Right string `json:"right"`

	// CommentLeft is the left comment delimiter (default: "{*")
	CommentLeft string `json:"comment_left"`

	// CommentRight is the right comment delimiter (default: "*}")
	CommentRight string `json:"comment_right"`
}

// SetConfig defines a template set configuration
type SetConfig struct {
	// Engine contains engine-specific configuration
	Engine EngineConfig `json:"engine"`
}

// initDefaults initializes default values for EngineConfig
func (e *EngineConfig) initDefaults() {
	// Default delimiters if not specified
	if e.Delimiters.Left == "" {
		e.Delimiters.Left = "{{"
	}
	if e.Delimiters.Right == "" {
		e.Delimiters.Right = "}}"
	}
	if e.Delimiters.CommentLeft == "" {
		e.Delimiters.CommentLeft = "{*"
	}
	if e.Delimiters.CommentRight == "" {
		e.Delimiters.CommentRight = "*}"
	}

	// Default extensions if not specified
	if len(e.Extensions) == 0 {
		e.Extensions = []string{".jet", ".html.jet", ".jet.html"}
	}

	// Initialize globals map if nil
	if e.Globals == nil {
		e.Globals = make(map[string]any)
	}
}

// Validate checks if the Config is valid
func (c *Config) Validate() error {
	if c.Source == "" {
		return ErrEmptySource
	}

	if c.Set.Name == "" {
		return ErrEmptySetName
	}

	return nil
}

// Validate checks if the EngineConfig is valid
func (e *EngineConfig) Validate() error {
	e.initDefaults()

	if e.Delimiters.Left == "" || e.Delimiters.Right == "" {
		return ErrEmptyDelimiters
	}

	if e.Delimiters.CommentLeft == "" || e.Delimiters.CommentRight == "" {
		return ErrEmptyCommentDelimiters
	}

	if e.Delimiters.Left == e.Delimiters.CommentLeft ||
		e.Delimiters.Right == e.Delimiters.CommentRight {
		return ErrConflictingDelimiters
	}

	if len(e.Extensions) == 0 {
		return ErrEmptyExtensions
	}

	return nil
}

// Validate checks if the SetConfig is valid
func (c *SetConfig) Validate() error {
	// Validate engine configuration
	return c.Engine.Validate()
}
