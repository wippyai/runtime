package requirementresolver

import (
	"fmt"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewResolver(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	assert.NotNil(t, resolver)
	assert.Equal(t, logger, resolver.logger)
}

func TestFindDependencyByParameterName(t *testing.T) {
	// Test the new simplified direct parameter matching approach
	nsDependencies := map[string]registry.Entry{
		"hello_world_dependency": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "hello_world_dependency",
			},
			Kind: registry.KindNamespaceDependency,
			Data: payload.New(map[string]interface{}{
				"parameters": []interface{}{
					map[string]interface{}{
						"name":  "API_ROUTER",
						"value": "system:api",
					},
					map[string]interface{}{
						"name":  "NAMESPACE",
						"value": "ns:system",
					},
				},
			}),
		},
	}

	testCases := []struct {
		requirementName string
		expectedValue   string
		shouldFind      bool
	}{
		{
			requirementName: "API_ROUTER",
			expectedValue:   "system:api",
			shouldFind:      true,
		},
		{
			requirementName: "NAMESPACE",
			expectedValue:   "ns:system",
			shouldFind:      true,
		},
		{
			requirementName: "NON_EXISTENT",
			expectedValue:   "",
			shouldFind:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Find parameter %s", tc.requirementName), func(t *testing.T) {
			dependency, paramValue, err := findDependencyByParameterName(tc.requirementName, nsDependencies)

			if tc.shouldFind {
				require.NoError(t, err, "Should find dependency parameter")
				assert.Equal(t, "hello_world_dependency", dependency.ID.Name)
				assert.Equal(t, tc.expectedValue, paramValue)
			} else {
				require.Error(t, err, "Should not find dependency parameter")
				assert.Contains(t, err.Error(), "no dependency parameter found")
			}
		})
	}
}
