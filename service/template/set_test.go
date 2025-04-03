package template

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/service/template"
	payloadSystem "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create test template set
func createTestSet(t *testing.T) *TemplateSet {
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
			Delimiters: template.DelimiterConfig{
				Left:  "<<",
				Right: ">>",
			},
			Globals: map[string]interface{}{
				"siteName": "Test Site",
				"version":  "1.0",
			},
		},
	}

	// Use real transcoder with json support
	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	id := registry.ID{Name: "test-set"}
	set, err := NewTemplateSet(id, cfg, transcoder)
	require.NoError(t, err)
	return set
}

func TestNewTemplateSet(t *testing.T) {
	// Create a test configuration
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
			Delimiters: template.DelimiterConfig{
				Left:  "<<",
				Right: ">>",
			},
			Globals: map[string]interface{}{
				"siteName": "Test Site",
				"version":  "1.0",
			},
		},
	}

	// Use real transcoder
	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	// Create a template set
	id := registry.ID{Name: "test-set"}
	set, err := NewTemplateSet(id, cfg, transcoder)
	require.NoError(t, err)
	require.NotNil(t, set)

	// Check that the set was created correctly
	assert.Equal(t, id, set.ID())
	assert.Equal(t, cfg, set.Config())
	assert.NotNil(t, set.jetSet)
	assert.NotNil(t, set.loader)
	assert.Empty(t, set.sources)
}

func TestTemplateSet_AddTemplate(t *testing.T) {
	// Create a template set
	set := createTestSet(t)

	// Add a template
	tmplID := registry.ID{Name: "test-template"}
	tmplSource := "Hello, << name >>!"
	err := set.AddTemplate(tmplID, tmplSource)
	require.NoError(t, err)

	// Verify the template was added
	sources := set.GetAllTemplates()
	assert.Len(t, sources, 1)
	assert.Equal(t, tmplSource, sources[tmplID])

	// Try to add the same template again
	err = set.AddTemplate(tmplID, tmplSource)
	assert.Error(t, err) // Should fail
}

func TestTemplateSet_UpdateTemplate(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplID := registry.ID{Name: "test-template"}
	err := set.AddTemplate(tmplID, "Hello, << name >>!")
	require.NoError(t, err)

	// Update the template
	newSource := "Hello, << name >>! You are << age >> years old."
	err = set.UpdateTemplate(tmplID, newSource)
	require.NoError(t, err)

	// Verify the template was updated
	sources := set.GetAllTemplates()
	assert.Equal(t, newSource, sources[tmplID])

	// Try to update a non-existent template
	nonExistentID := registry.ID{Name: "non-existent"}
	err = set.UpdateTemplate(nonExistentID, "Test")
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_RemoveTemplate(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplID := registry.ID{Name: "test-template"}
	err := set.AddTemplate(tmplID, "Hello, << name >>!")
	require.NoError(t, err)

	// Remove the template
	err = set.RemoveTemplate(tmplID)
	require.NoError(t, err)

	// Verify the template was removed
	sources := set.GetAllTemplates()
	assert.Empty(t, sources)

	// Try to remove a non-existent template
	err = set.RemoveTemplate(tmplID)
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_GetTemplateSource(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplID := registry.ID{Name: "test-template"}
	tmplSource := "Hello, << name >>!"
	err := set.AddTemplate(tmplID, tmplSource)
	require.NoError(t, err)

	// Get the template source
	source, err := set.GetTemplateSource(tmplID)
	require.NoError(t, err)
	assert.Equal(t, tmplSource, source)

	// Try to get a non-existent template
	nonExistentID := registry.ID{Name: "non-existent"}
	_, err = set.GetTemplateSource(nonExistentID)
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_RenderTemplate(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplID := registry.ID{Name: "test-template"}
	tmplSource := "Hello, << name >>! Site: << siteName >>"
	err := set.AddTemplate(tmplID, tmplSource)
	require.NoError(t, err)

	// Render the template with variables
	vars := map[string]interface{}{
		"name": "John",
	}
	result, err := set.RenderTemplate(tmplID, vars)
	require.NoError(t, err)
	assert.Equal(t, "Hello, John! Site: Test Site", result)

	// Try to render a non-existent template
	nonExistentID := registry.ID{Name: "non-existent"}
	_, err = set.RenderTemplate(nonExistentID, vars)
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_RenderPayload(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplID := registry.ID{Name: "test-template"}
	tmplSource := "Hello, << name >>! Age: << age >>"
	err := set.AddTemplate(tmplID, tmplSource)
	require.NoError(t, err)

	// Create a JSON payload
	jsonData := `{"name":"Jane","age":30}`
	p := payload.NewPayload(jsonData, payload.JSON)

	// Render the template with the payload
	result, err := set.RenderPayload(tmplID, p)
	require.NoError(t, err)
	assert.Equal(t, "Hello, Jane! Age: 30", result.Data())

	// Try to render a non-existent template
	nonExistentID := registry.ID{Name: "non-existent"}
	_, err = set.RenderPayload(nonExistentID, p)
	assert.Error(t, err)
}

func TestTemplateSet_Acquire(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplID := registry.ID{Name: "test-template"}
	err := set.AddTemplate(tmplID, "Hello, << name >>!")
	require.NoError(t, err)

	// Acquire the set itself
	ctx := context.Background()
	setResource, err := set.Acquire(ctx, set.ID(), resource.ModeNormal)
	require.NoError(t, err)

	// Get the set from the resource
	acquiredSet, err := setResource.Get()
	require.NoError(t, err)
	assert.Equal(t, set, acquiredSet)

	// Acquire a template
	tmplResource, err := set.Acquire(ctx, tmplID, resource.ModeNormal)
	require.NoError(t, err)

	// Get the template from the resource
	acquiredTemplate, err := tmplResource.Get()
	require.NoError(t, err)

	// Check the template details
	tmplDetails, ok := acquiredTemplate.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, tmplID, tmplDetails["id"])
	assert.Equal(t, set.ID(), tmplDetails["setId"])

	// Try to acquire a non-existent template
	nonExistentID := registry.ID{Name: "non-existent"}
	_, err = set.Acquire(ctx, nonExistentID, resource.ModeNormal)
	assert.Equal(t, ErrTemplateNotFound, err)

	// Try to acquire with unsupported mode
	_, err = set.Acquire(ctx, set.ID(), resource.ModeExclusive)
	assert.Error(t, err)
}

func TestTemplateSet_ResourceRelease(t *testing.T) {
	// Create a template set
	set := createTestSet(t)

	// Acquire the set
	ctx := context.Background()
	resource, err := set.Acquire(ctx, set.ID(), resource.ModeNormal)
	require.NoError(t, err)

	// Get the set
	_, err = resource.Get()
	require.NoError(t, err)

	// Release the resource
	resource.Release()

	// Try to get after release
	_, err = resource.Get()
	assert.Error(t, err)
}
