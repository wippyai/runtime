package template

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
)

// Registry kind constants for Template components
const (
	// KindTemplate identifies a template component
	KindTemplate registry.Kind = "template.jet"
	// KindTemplateSet identifies a template set component
	KindTemplateSet registry.Kind = "template.set"
)

// Config represents configuration for a template entry
type Config struct {
	Meta registry.Metadata `json:"meta"`

	// Source defines the template content or location
	Source string `json:"source"`

	// Set is the template set this template belongs to
	Set registry.ID `json:"set"`
}

// EngineConfig contains Jet template engine configuration
type EngineConfig struct {
	// DevelopmentMode disables caching when true
	DevelopmentMode bool `json:"development_mode"`

	// Delimiters customizes template variable delimiters
	Delimiters DelimiterConfig `json:"delimiters"`

	// Extensions defines file extensions to try when resolving templates
	Extensions []string `json:"extensions"`

	// Globals defines global variables available to all templates
	Globals map[string]interface{} `json:"globals"`
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

// InitDefaults initializes default values for Config
func (c *Config) InitDefaults() {
	// Ensure Set has a valid NS if not specified
	if c.Set.NS == "" {
		c.Set.NS = "system.template"
	}

	// Default to the default set if not specified
	if c.Set.Name == "" {
		c.Set.Name = "default"
	}
}

// InitDefaults initializes default values for EngineConfig
func (e *EngineConfig) InitDefaults() {
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
		e.Globals = make(map[string]interface{})
	}
}

// InitDefaults initializes default values for SetConfig
func (c *SetConfig) InitDefaults() {
	// Initialize engine defaults
	c.Engine.InitDefaults()
}

// Validate checks if the Config is valid
func (c *Config) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("template source cannot be empty")
	}

	// Validate template set
	if c.Set.Name == "" {
		return fmt.Errorf("template set name cannot be empty")
	}

	return nil
}

// Validate checks if the EngineConfig is valid
func (e *EngineConfig) Validate() error {
	// Validate delimiters
	if e.Delimiters.Left == "" || e.Delimiters.Right == "" {
		return fmt.Errorf("template delimiters cannot be empty")
	}

	if e.Delimiters.CommentLeft == "" || e.Delimiters.CommentRight == "" {
		return fmt.Errorf("comment delimiters cannot be empty")
	}

	// Ensure delimiters don't conflict
	if e.Delimiters.Left == e.Delimiters.CommentLeft ||
		e.Delimiters.Right == e.Delimiters.CommentRight {
		return fmt.Errorf("template and comment delimiters must be different")
	}

	// Validate extensions
	if len(e.Extensions) == 0 {
		return fmt.Errorf("template extensions cannot be empty")
	}

	return nil
}

// Validate checks if the SetConfig is valid
func (c *SetConfig) Validate() error {
	// Validate engine configuration
	return c.Engine.Validate()
}
