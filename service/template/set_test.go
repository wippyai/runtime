package template

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"

	"github.com/CloudyKit/jet/v6"
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
			Globals: map[string]any{
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

// Helper to create test template set
func createTestSetDefault(t *testing.T) *TemplateSet {
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
			Delimiters: template.DelimiterConfig{
				Left:  "{{",
				Right: "}}",
			},
			Globals: map[string]any{
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
			Globals: map[string]any{
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
	tmplName := "test-template"
	tmplSource := "Hello, << name >>!"
	err := set.AddTemplate(tmplName, tmplSource)
	require.NoError(t, err)

	// Verify the template was added
	sources := set.GetAllTemplates()
	assert.Len(t, sources, 1)
	assert.Equal(t, tmplSource, sources[tmplName])

	// Try to add the same template again
	err = set.AddTemplate(tmplName, tmplSource)
	assert.Error(t, err) // Should fail
}

func TestTemplateSet_UpdateTemplate(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplName := "test-template"
	err := set.AddTemplate(tmplName, "Hello, << name >>!")
	require.NoError(t, err)

	// Update the template
	newSource := "Hello, << name >>! You are << age >> years old."
	err = set.UpdateTemplate(tmplName, newSource)
	require.NoError(t, err)

	// Verify the template was updated
	sources := set.GetAllTemplates()
	assert.Equal(t, newSource, sources[tmplName])

	// Try to update a non-existent template
	nonExistentName := "non-existent"
	err = set.UpdateTemplate(nonExistentName, "Test")
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_RemoveTemplate(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplName := "test-template"
	err := set.AddTemplate(tmplName, "Hello, << name >>!")
	require.NoError(t, err)

	// Remove the template
	err = set.RemoveTemplate(tmplName)
	require.NoError(t, err)

	// Verify the template was removed
	sources := set.GetAllTemplates()
	assert.Empty(t, sources)

	// Try to remove a non-existent template
	err = set.RemoveTemplate(tmplName)
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_GetTemplateSource(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplName := "test-template"
	tmplSource := "Hello, << name >>!"
	err := set.AddTemplate(tmplName, tmplSource)
	require.NoError(t, err)

	// Get the template source
	source, err := set.GetTemplateSource(tmplName)
	require.NoError(t, err)
	assert.Equal(t, tmplSource, source)

	// Try to get a non-existent template
	nonExistentName := "non-existent"
	_, err = set.GetTemplateSource(nonExistentName)
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_RenderTemplate(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplName := "test-template"
	tmplSource := "Hello, << name >>! Site: << siteName >>"
	err := set.AddTemplate(tmplName, tmplSource)
	require.NoError(t, err)

	// Render the template with variables
	vars := map[string]any{
		"name": "John",
	}
	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Hello, John! Site: Test Site", result)

	// Try to render a non-existent template
	nonExistentName := "non-existent"
	_, err = set.RenderTemplate(nonExistentName, vars)
	assert.Equal(t, ErrTemplateNotFound, err)
}

func TestTemplateSet_RenderPayload(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplName := "test-template"
	tmplSource := "Hello, << name >>! Age: << age >>"
	err := set.AddTemplate(tmplName, tmplSource)
	require.NoError(t, err)

	// Create a JSON payload
	jsonData := `{"name":"Jane","age":30}`
	p := payload.NewPayload(jsonData, payload.JSON)

	// Render the template with the payload
	result, err := set.RenderPayload(tmplName, p)
	require.NoError(t, err)
	assert.Equal(t, "Hello, Jane! Age: 30", result)

	// Try to render a non-existent template
	nonExistentName := "non-existent"
	_, err = set.RenderPayload(nonExistentName, p)
	assert.Error(t, err)
}

func TestTemplateSet_Acquire(t *testing.T) {
	// Create a template set with a template
	set := createTestSet(t)
	tmplName := "test-template"
	err := set.AddTemplate(tmplName, "Hello, << name >>!")
	require.NoError(t, err)

	// Acquire the set itself
	ctx := ctxapi.NewRootContext()
	setResource, err := set.Acquire(ctx, set.ID(), resource.ModeNormal)
	require.NoError(t, err)

	// Get the set from the resource
	acquiredSet, err := setResource.Get()
	require.NoError(t, err)
	assert.Equal(t, set, acquiredSet)
}

func TestTemplateSet_ResourceRelease(t *testing.T) {
	// Create a template set
	set := createTestSet(t)

	// Acquire the set
	ctx := ctxapi.NewRootContext()
	res, err := set.Acquire(ctx, set.ID(), resource.ModeNormal)
	require.NoError(t, err)

	// Get the set
	_, err = res.Get()
	require.NoError(t, err)

	// Release the resource
	res.Release()

	// Try to get after release
	_, err = res.Get()
	assert.Equal(t, resource.ErrResourceReleased, err)
}

func TestTemplateInclusion(t *testing.T) {
	set := createTestSet(t)

	// Add a base template
	baseTemplateName := "base"
	baseSource := "Header: << siteName >>"
	require.NoError(t, set.AddTemplate(baseTemplateName, baseSource))

	// Add a template that includes the base - use just the template name
	mainTemplateName := "main"
	mainSource := "Content: << name >> << include \"base\" >>"
	require.NoError(t, set.AddTemplate(mainTemplateName, mainSource))

	// Render with inclusion
	vars := map[string]any{"name": "John"}
	result, err := set.RenderTemplate(mainTemplateName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Content: John Header: Test Site", result)
}

func TestTemplateInheritance(t *testing.T) {
	set := createTestSet(t)

	// Fix: Use proper block syntax with parentheses and avoid 'content' keyword
	layoutName := "layout"
	layoutSource := "<!DOCTYPE html><html><head><title><< yield title() >></title></head><body><< yield body() >></body></html>"
	require.NoError(t, set.AddTemplate(layoutName, layoutSource))

	// Add a page template that extends the layout - use parentheses for block names
	pageName := "page"
	pageSource := "<< extends \"layout\" >>\n<< block title() >><< siteName >><< end >>\n<< block body() >>Welcome, << name >>!<< end >>"
	require.NoError(t, set.AddTemplate(pageName, pageSource))

	// Render with inheritance
	vars := map[string]any{"name": "Alice"}
	result, err := set.RenderTemplate(pageName, vars)
	require.NoError(t, err)
	assert.Equal(t, "<!DOCTYPE html><html><head><title>Test Site</title></head><body>Welcome, Alice!</body></html>", result)
}

func TestBuiltinFunctions(t *testing.T) {
	set := createTestSet(t)

	// Test len, upper, lower functions
	tmplName := "funcs"
	tmplSource := "Items: << len(items) >>, Upper: << upper(name) >>, Lower: << lower(name) >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	vars := map[string]any{
		"name":  "John",
		"items": []string{"a", "b", "c"},
	}

	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Items: 3, Upper: JOHN, Lower: john", result)
}

func TestCustomFunctions(t *testing.T) {
	// Create a set with custom function
	cfg := &template.SetConfig{
		Engine: template.EngineConfig{
			DevelopmentMode: true,
			Delimiters: template.DelimiterConfig{
				Left:  "<<",
				Right: ">>",
			},
			Globals: map[string]any{
				"siteName": "Test Site",
			},
		},
	}

	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	id := registry.ID{Name: "test-set"}
	set, err := NewTemplateSet(id, cfg, transcoder)
	require.NoError(t, err)

	// Add a custom function
	set.jetSet.AddGlobalFunc("reverse", func(a jet.Arguments) reflect.Value {
		args := a.Get(0).String()
		runes := []rune(args)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return reflect.ValueOf(string(runes))
	})

	// Add template using the function
	tmplName := "custom-func"
	tmplSource := "Reversed: << reverse(name) >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Render
	vars := map[string]any{"name": "Hello"}
	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Reversed: olleH", result)
}

func TestConditionals(t *testing.T) {
	set := createTestSet(t)

	tmplName := "cond"
	tmplSource := "<< if age > 18 >>Adult<< else >>Minor<< end >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Test true condition
	result, err := set.RenderTemplate(tmplName, map[string]any{"age": 25})
	require.NoError(t, err)
	assert.Equal(t, "Adult", result)

	// Test false condition
	result, err = set.RenderTemplate(tmplName, map[string]any{"age": 16})
	require.NoError(t, err)
	assert.Equal(t, "Minor", result)
}

func TestLoops(t *testing.T) {
	set := createTestSet(t)

	tmplName := "loop"
	tmplSource := "Items: << range i, item := items >><< if i > 0 >>, << end >><< item >><< end >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Test range over slice
	vars := map[string]any{
		"items": []string{"a", "b", "c"},
	}

	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Items: a, b, c", result)
}

func TestNestedData(t *testing.T) {
	set := createTestSet(t)

	tmplName := "nested"
	tmplSource := "User: << user.name >>, City: << user.address.city >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Test nested maps
	vars := map[string]any{
		"user": map[string]any{
			"name": "John",
			"address": map[string]any{
				"city": "New York",
			},
		},
	}

	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "User: John, City: New York", result)
}

func TestTemplateError(t *testing.T) {
	set := createTestSet(t)

	// Add template with an error - a function that does not exist
	tmplName := "error-template"
	tmplSource := "<< unknownFunction() >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Rendering should fail
	_, err := set.RenderTemplate(tmplName, nil)
	assert.Error(t, err)
}

func TestConcurrentRendering(t *testing.T) {
	set := createTestSet(t)

	// Add a template
	tmplName := "concurrent"
	tmplSource := "Hello, << name >>!"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Render concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vars := map[string]any{
				"name": fmt.Sprintf("User%d", i),
			}
			result, err := set.RenderTemplate(tmplName, vars)
			if err != nil {
				t.Errorf("Failed to render template in goroutine %d: %v", i, err)
				return
			}
			if result != fmt.Sprintf("Hello, User%d!", i) {
				t.Errorf("Unexpected result in goroutine %d: %s", i, result)
			}
		}(i)
	}

	wg.Wait()
}

func TestTemplateSet_ConcurrentOperations(t *testing.T) {
	set := createTestSet(t)

	// Add an initial template
	tmplName := "base"
	tmplSource := "Hello, << name >>!"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Perform concurrent operations
	var wg sync.WaitGroup
	concurrency := 20
	errChan := make(chan error, concurrency*4) // Buffer for potential errors

	// Goroutines for adding templates
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("template-%d", i)
			err := set.AddTemplate(name, "Template content << i >>")
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	// Goroutines for reading templates
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := set.GetTemplateSource("base")
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Goroutines for rendering templates
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vars := map[string]any{
				"name": "Concurrent User",
			}
			_, err := set.RenderTemplate("base", vars)
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Goroutines for updating templates (some will fail as expected)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("template-%d", i)
			err := set.UpdateTemplate(name, "Updated content << i >>")
			if err != nil && !errors.Is(err, ErrTemplateNotFound) {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check if there were any unexpected errors
	for err := range errChan {
		t.Errorf("Unexpected error during concurrent operations: %v", err)
	}

	// Verify that all templates were added
	templates := set.GetAllTemplates()
	assert.Equal(t, concurrency+1, len(templates)) // +1 for the base template
}

func TestTemplateSet_TemplateCacheInvalidation(t *testing.T) {
	set := createTestSet(t)

	// Add a template
	tmplName := "cached"
	tmplSource := "Hello, << name >>!"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Render it once to ensure it's cached by Jet
	vars := map[string]any{
		"name": "Initial",
	}
	result1, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Hello, Initial!", result1)

	// Update the template
	newSource := "Greetings, << name >>!"
	require.NoError(t, set.UpdateTemplate(tmplName, newSource))

	// Render again to check if the cache was invalidated
	result2, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Greetings, Initial!", result2)
}

func TestTemplateSet_RenderWithInvalidTemplate(t *testing.T) {
	set := createTestSet(t)

	// Add a template with invalid syntax
	tmplName := "invalid"
	// Missing end tag for the if statement
	tmplSource := "<< if condition >>Hello<< name >>"
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Rendering should fail
	vars := map[string]any{
		"condition": true,
		"name":      "World",
	}
	_, err := set.RenderTemplate(tmplName, vars)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get compiled template")
}

func TestTemplateSet_RenderWithUndefinedVariables(t *testing.T) {
	set := createTestSet(t)

	// Add a template that references variables
	tmplName := "vars"
	tmplSource := "Hello, << name >>! Your age is << age >>."
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Test with missing variables
	vars := map[string]any{
		"name": "Alice",
		// age is missing
	}
	_, err := set.RenderTemplate(tmplName, vars)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestTemplateSet_RenderWithComplexNestedData(t *testing.T) {
	set := createTestSet(t)

	// Add a template that processes complex nested data
	tmplName := "complex-data"
	tmplSource := `
	<< range users >>
		<div class="user">
			<h2><< .name >></h2>
			<p>Email: << .email >></p>
			<p>Age: << .age >></p>
			<h3>Addresses:</h3>
			<ul>
				<< range .addresses >>
					<li>
						<< .type >>: << .street >>, << .city >>, << .zip >>
					</li>
				<< end >>
			</ul>
			<h3>Orders:</h3>
			<< if len(.orders) == 0 >>
				<p>No orders</p>
			<< else >>
				<table>
					<tr><th>ID</th><th>Date</th><th>Total</th></tr>
					<< range .orders >>
						<tr>
							<td><< .id >></td>
							<td><< .date >></td>
							<td>$<< .total >></td>
						</tr>
					<< end >>
				</table>
			<< end >>
		</div>
	<< end >>
	`
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Create complex test data
	vars := map[string]any{
		"users": []map[string]any{
			{
				"name":  "John Doe",
				"email": "john@example.com",
				"age":   30,
				"addresses": []map[string]any{
					{
						"type":   "Home",
						"street": "123 Main St",
						"city":   "New York",
						"zip":    "10001",
					},
					{
						"type":   "Work",
						"street": "456 Park Ave",
						"city":   "New York",
						"zip":    "10022",
					},
				},
				"orders": []map[string]any{
					{
						"id":    "ORD-001",
						"date":  "2023-01-15",
						"total": "125.99",
					},
					{
						"id":    "ORD-002",
						"date":  "2023-02-20",
						"total": "89.50",
					},
				},
			},
			{
				"name":  "Jane Smith",
				"email": "jane@example.com",
				"age":   25,
				"addresses": []map[string]any{
					{
						"type":   "Home",
						"street": "789 Broadway",
						"city":   "Boston",
						"zip":    "02116",
					},
				},
				"orders": []map[string]any{},
			},
		},
	}

	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)

	// Validate output contains expected elements
	assert.Contains(t, result, "John Doe")
	assert.Contains(t, result, "jane@example.com")
	assert.Contains(t, result, "Home: 123 Main St, New York, 10001")
	assert.Contains(t, result, "Work: 456 Park Ave, New York, 10022")
	assert.Contains(t, result, "ORD-001")
	assert.Contains(t, result, "$125.99")
	assert.Contains(t, result, "No orders") // For Jane who has no orders
}

// TestCommentHandling tests Jet template comments
func TestCommentHandling(t *testing.T) {
	set := createTestSetDefault(t)

	// Add a template with comments
	tmplName := "comments"
	tmplSource := `Hello, 
	{* This is a comment and should not be rendered *}
	{{ name }}!
	{* 
		Multi-line comment
		{{ ignored }}
		More text
	*}
	Still here.`
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	vars := map[string]any{"name": "John", "ignored": "SHOULD NOT APPEAR"}
	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)

	expected := `Hello, 
	
	John!
	
	Still here.`
	assert.Equal(t, expected, result)
	assert.NotContains(t, result, "SHOULD NOT APPEAR")
	assert.NotContains(t, result, "comment")
}

// TestTryCatchBlocks tests the try/catch error handling in templates
func TestTryCatchBlocks(t *testing.T) {
	set := createTestSetDefault(t)

	// Add a template with try/catch blocks
	tmplName := "try-catch"
	tmplSource := `Start
	{{ try }}
		{{ undefinedVar }}
		This should be skipped
	{{ catch }}
		Error caught!
	{{ end }}
	End`
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Add a template that captures the error message
	tmplName2 := "try-catch-error"
	tmplSource2 := `Start
	{{ try }}
		{{ undefinedVar }}
		This should be skipped
	{{ catch err }}
		Error: {{ err.Error() }}
	{{ end }}
	End`
	require.NoError(t, set.AddTemplate(tmplName2, tmplSource2))

	// Test basic try/catch
	result, err := set.RenderTemplate(tmplName, nil)
	require.NoError(t, err)
	assert.Contains(t, result, "Error caught!")
	assert.NotContains(t, result, "This should be skipped")

	// Test try/catch with error capture
	result, err = set.RenderTemplate(tmplName2, nil)
	require.NoError(t, err)
	assert.Contains(t, result, "Error:")
	assert.NotContains(t, result, "This should be skipped")
}

// TestAdvancedExpressions tests various advanced expression features
func TestAdvancedExpressions(t *testing.T) {
	set := createTestSetDefault(t)

	// Test ternary operator
	tmplName := "ternary"
	tmplSource := `{{ hasTitle ? title : "Default Title" }}`
	require.NoError(t, set.AddTemplate(tmplName, tmplSource))

	// Test string concatenation
	tmplName2 := "concat"
	tmplSource2 := `{{ first + " " + last }}`
	require.NoError(t, set.AddTemplate(tmplName2, tmplSource2))

	// Test slicing
	tmplName3 := "slice"
	tmplSource3 := `{{ items[1:3] }}`
	require.NoError(t, set.AddTemplate(tmplName3, tmplSource3))

	// Test ternary
	vars := map[string]any{"hasTitle": true, "title": "Custom Title"}
	result, err := set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Custom Title", result)

	vars = map[string]any{"hasTitle": false, "title": "Custom Title"}
	result, err = set.RenderTemplate(tmplName, vars)
	require.NoError(t, err)
	assert.Equal(t, "Default Title", result)

	// Test string concatenation
	vars = map[string]any{"first": "John", "last": "Doe"}
	result, err = set.RenderTemplate(tmplName2, vars)
	require.NoError(t, err)
	assert.Equal(t, "John Doe", result)

	// Test slicing - the template will render the Go representation of the slice
	vars = map[string]any{"items": []string{"a", "b", "c", "d", "e"}}
	result, err = set.RenderTemplate(tmplName3, vars)
	require.NoError(t, err)
	assert.Contains(t, result, "b")
	assert.Contains(t, result, "c")
	assert.NotContains(t, result, "a")
	assert.NotContains(t, result, "d")
}

// TestImportStatement tests the import feature
func TestImportStatement(t *testing.T) {
	set := createTestSetDefault(t)

	// First, add a template with block definitions to import
	blocksTemplate := "blocks"
	blocksSource := `
	{{ block header(title) }}
		<header>
			<h1>{{ title }}</h1>
		</header>
	{{ end }}
	
	{{ block footer() }}
		<footer>&copy; 2023</footer>
	{{ end }}
	`
	require.NoError(t, set.AddTemplate(blocksTemplate, blocksSource))

	// Now add a template that imports the blocks
	mainTemplate := "main-with-import"
	mainSource := `
	{{ import "blocks" }}
	
	<!DOCTYPE html>
	<html>
	<body>
		{{ yield header(title="Page Title") }}
		
		<main>Content here</main>
		
		{{ yield footer() }}
	</body>
	</html>
	`
	require.NoError(t, set.AddTemplate(mainTemplate, mainSource))

	// Render the template with imports
	result, err := set.RenderTemplate(mainTemplate, nil)
	require.NoError(t, err)

	// Verify that blocks from the imported template were rendered
	assert.Contains(t, result, "<header>")
	assert.Contains(t, result, "<h1>Page Title</h1>")
	assert.Contains(t, result, "<footer>&copy; 2023</footer>")
}

// TestErrorPropagation tests how errors bubble up from nested templates
func TestErrorPropagation(t *testing.T) {
	set := createTestSetDefault(t)

	// Add a template with an error
	errorTemplate := "error-template"
	errorSource := `{{ undefinedVar }}`
	require.NoError(t, set.AddTemplate(errorTemplate, errorSource))

	// Add a template that includes the error template
	includeTemplate := "include-error"
	includeSource := `
	Before include.
	{{ include "error-template" }}
	After include.
	`
	require.NoError(t, set.AddTemplate(includeTemplate, includeSource))

	// Add a template that extends with errors
	baseTemplate := "base-template"
	baseSource := `
	<!DOCTYPE html>
	<html>
	<body>
		{{ yield content() }}
	</body>
	</html>
	`
	require.NoError(t, set.AddTemplate(baseTemplate, baseSource))

	extendTemplate := "extend-error"
	extendSource := `
	{{ extends "base-template" }}
	
	{{ block content() }}
		{{ undefinedVar }}
	{{ end }}
	`
	require.NoError(t, set.AddTemplate(extendTemplate, extendSource))

	// Add a nested include chain
	nested1 := "nested1"
	nested1Source := `Start nested1 {{ include "nested2" }} End nested1`
	require.NoError(t, set.AddTemplate(nested1, nested1Source))

	nested2 := "nested2"
	nested2Source := `Start nested2 {{ include "nested3" }} End nested2`
	require.NoError(t, set.AddTemplate(nested2, nested2Source))

	nested3 := "nested3"
	nested3Source := `Start nested3 {{ undefinedVar }} End nested3`
	require.NoError(t, set.AddTemplate(nested3, nested3Source))

	// Test direct error
	_, err := set.RenderTemplate(errorTemplate, nil)
	assert.Error(t, err)

	// Test error in included template
	_, err = set.RenderTemplate(includeTemplate, nil)
	assert.Error(t, err)

	// Test error in extended template
	_, err = set.RenderTemplate(extendTemplate, nil)
	assert.Error(t, err)

	// Test deeply nested error
	_, err = set.RenderTemplate(nested1, nil)
	assert.Error(t, err)

	// The error should contain a trace of the template hierarchy
	assert.Contains(t, err.Error(), "nested")
}
