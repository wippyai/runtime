package deps

import (
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestPayload is a test implementation of registry.Payload
type TestPayload struct {
	data interface{}
}

func (p *TestPayload) Data() interface{} {
	return p.data
}

func (p *TestPayload) Format() payload.Format {
	return payload.Golang
}

func TestCreateParentDependencyMap(t *testing.T) {
	logger := zap.NewNop()

	t.Run("empty entries and load result", func(t *testing.T) {
		entries := []registry.Entry{}
		loadResult := &LoadResult{Modules: []LoadedModule{}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Empty(t, parentMap)
	})

	t.Run("nil load result", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "igor-test-3/test-2",
				}},
			},
		}

		parentMap := CreateParentDependencyMap(entries, nil, logger)
		assert.Empty(t, parentMap)
	})

	t.Run("no matching dependency entries", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "igor-test-3/test-2",
				}},
			},
		}
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "igor-test-3", Module: "test-1"}},
		}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Empty(t, parentMap)
	})

	t.Run("matching dependency entries with parameters", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "igor-test-3/test-2",
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "API_ROUTER",
							"value": "http://localhost:8080",
						},
					},
				}},
			},
		}
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "igor-test-3", Module: "test-2"}},
		}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Len(t, parentMap, 1)
		assert.Contains(t, parentMap, "igor-test-3/test-2")
		assert.Len(t, parentMap["igor-test-3/test-2"], 1)
		assert.Equal(t, "app:test_dependency", parentMap["igor-test-3/test-2"][0].EntryID)
		assert.Equal(t, map[string]string{"API_ROUTER": "http://localhost:8080"}, parentMap["igor-test-3/test-2"][0].Parameters)
	})

	t.Run("multiple dependencies for same module", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "igor-test-3/test-2",
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "API_ROUTER",
							"value": "http://localhost:8080",
						},
					},
				}},
			},
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "igor-test-3/test-2",
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "NAMESPACE",
							"value": "test-namespace",
						},
					},
				}},
			},
		}
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "igor-test-3", Module: "test-2"}},
		}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Len(t, parentMap, 1)
		assert.Contains(t, parentMap, "igor-test-3/test-2")
		assert.Len(t, parentMap["igor-test-3/test-2"], 2)
	})

	t.Run("non-dependency entries are ignored", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceRequirement,
				Data: &TestPayload{data: map[string]interface{}{
					"name": "API_ROUTER",
				}},
			},
		}
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "igor-test-3", Module: "test-2"}},
		}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Empty(t, parentMap)
	})

	t.Run("entries without component field are ignored", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"name": "test",
				}},
			},
		}
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "igor-test-3", Module: "test-2"}},
		}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Empty(t, parentMap)
	})

	t.Run("entries with non-string component are ignored", func(t *testing.T) {
		entries := []registry.Entry{
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": 123,
				}},
			},
		}
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "igor-test-3", Module: "test-2"}},
		}}

		parentMap := CreateParentDependencyMap(entries, loadResult, logger)
		assert.Empty(t, parentMap)
	})
}

func TestSelectBestParentDependency(t *testing.T) {
	logger := zap.NewNop()

	t.Run("no parent dependencies", func(t *testing.T) {
		requirement := registry.Entry{
			Kind: registry.KindNamespaceRequirement,
		}
		parentDeps := []ParentDependencyInfo{}

		result := SelectBestParentDependency(requirement, parentDeps, logger)
		assert.Empty(t, result)
	})

	t.Run("single parent dependency", func(t *testing.T) {
		requirement := registry.Entry{
			Kind: registry.KindNamespaceRequirement,
		}
		parentDeps := []ParentDependencyInfo{
			{
				EntryID:    "app:test_dependency",
				Parameters: map[string]string{"API_ROUTER": "http://localhost:8080"},
			},
		}

		result := SelectBestParentDependency(requirement, parentDeps, logger)
		assert.Equal(t, "app:test_dependency", result)
	})

	t.Run("multiple parent dependencies with parameter match", func(t *testing.T) {
		requirement := registry.Entry{
			Kind: registry.KindNamespaceRequirement,
		}
		parentDeps := []ParentDependencyInfo{
			{
				EntryID:    "app:test_dependency1",
				Parameters: map[string]string{"NAMESPACE": "test-namespace"},
			},
			{
				EntryID:    "app:test_dependency2",
				Parameters: map[string]string{"API_ROUTER": "http://localhost:8080"},
			},
		}

		result := SelectBestParentDependency(requirement, parentDeps, logger)
		assert.Equal(t, "app:test_dependency2", result)
	})

	t.Run("multiple parent dependencies without parameter match", func(t *testing.T) {
		requirement := registry.Entry{
			Kind: registry.KindNamespaceRequirement,
		}
		parentDeps := []ParentDependencyInfo{
			{
				EntryID:    "app:test_dependency1",
				Parameters: map[string]string{"NAMESPACE": "test-namespace"},
			},
			{
				EntryID:    "app:test_dependency2",
				Parameters: map[string]string{"DATABASE": "postgresql"},
			},
		}

		result := SelectBestParentDependency(requirement, parentDeps, logger)
		assert.Equal(t, "app:test_dependency1", result) // Should return first available
	})
}

func TestValidateParentDependencyConflicts(t *testing.T) {
	logger := zap.NewNop()

	t.Run("no conflicts with single parent", func(t *testing.T) {
		parentMap := map[string][]ParentDependencyInfo{
			"igor-test-3/test-2": {
				{
					EntryID:    "app:test_dependency1",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:8080"},
				},
			},
		}

		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.NoError(t, err)
	})

	t.Run("no conflicts with multiple parents but different parameters", func(t *testing.T) {
		parentMap := map[string][]ParentDependencyInfo{
			"igor-test-3/test-2": {
				{
					EntryID:    "app:test_dependency1",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:8080"},
				},
				{
					EntryID:    "app:test_dependency2",
					Parameters: map[string]string{"NAMESPACE": "test-namespace"},
				},
			},
		}

		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.NoError(t, err)
	})

	t.Run("conflict detected with same parameter", func(t *testing.T) {
		parentMap := map[string][]ParentDependencyInfo{
			"igor-test-3/test-2": {
				{
					EntryID:    "app:test_dependency1",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:8080"},
				},
				{
					EntryID:    "app:test_dependency2",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:9090"},
				},
			},
		}

		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conflict: multiple parent dependencies for module igor-test-3/test-2 define parameter API_ROUTER")
	})

	t.Run("conflict detected with multiple parameters", func(t *testing.T) {
		parentMap := map[string][]ParentDependencyInfo{
			"igor-test-3/test-2": {
				{
					EntryID:    "app:test_dependency1",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:8080", "NAMESPACE": "ns1"},
				},
				{
					EntryID:    "app:test_dependency2",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:9090", "NAMESPACE": "ns2"},
				},
			},
		}

		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conflict: multiple parent dependencies for module igor-test-3/test-2 define parameter")
	})

	t.Run("multiple modules with conflicts", func(t *testing.T) {
		parentMap := map[string][]ParentDependencyInfo{
			"igor-test-3/test-1": {
				{
					EntryID:    "app:test_dependency1",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:8080"},
				},
				{
					EntryID:    "app:test_dependency2",
					Parameters: map[string]string{"API_ROUTER": "http://localhost:9090"},
				},
			},
			"igor-test-3/test-2": {
				{
					EntryID:    "app:test_dependency3",
					Parameters: map[string]string{"NAMESPACE": "ns1"},
				},
			},
		}

		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conflict: multiple parent dependencies for module igor-test-3/test-1 define parameter API_ROUTER")
	})

	t.Run("empty parent map", func(t *testing.T) {
		parentMap := map[string][]ParentDependencyInfo{}

		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.NoError(t, err)
	})
}

func TestComplexIntegrationScenario(t *testing.T) {
	logger := zap.NewNop()

	t.Run("complex scenario with 10 dependencies and 5 requirements", func(t *testing.T) {
		// Create 10 different dependency entries with various parameters
		entries := []registry.Entry{
			// Module 1: web-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/web-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "API_ROUTER", "value": "http://localhost:8080"},
						map[string]interface{}{"name": "DATABASE_URL", "value": "postgres://localhost:5432/webdb"},
					},
				}},
			},
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/web-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "CACHE_URL", "value": "redis://localhost:6379"},
					},
				}},
			},
			// Module 2: auth-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/auth-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "JWT_SECRET", "value": "super-secret-key"},
						map[string]interface{}{"name": "AUTH_DB", "value": "postgres://localhost:5432/authdb"},
					},
				}},
			},
			// Module 3: payment-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/payment-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "STRIPE_KEY", "value": "sk_test_123456789"},
						map[string]interface{}{"name": "PAYMENT_DB", "value": "postgres://localhost:5432/paymentdb"},
					},
				}},
			},
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/payment-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "WEBHOOK_URL", "value": "https://api.company.com/webhooks"},
					},
				}},
			},
			// Module 4: notification-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/notification-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "SMTP_HOST", "value": "smtp.company.com"},
						map[string]interface{}{"name": "SMS_API_KEY", "value": "sms_api_key_123"},
					},
				}},
			},
			// Module 5: analytics-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/analytics-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "ANALYTICS_DB", "value": "postgres://localhost:5432/analyticsdb"},
						map[string]interface{}{"name": "KAFKA_BROKERS", "value": "localhost:9092"},
					},
				}},
			},
			// Module 6: file-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/file-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "S3_BUCKET", "value": "company-files"},
						map[string]interface{}{"name": "S3_REGION", "value": "us-east-1"},
					},
				}},
			},
			// Module 7: search-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/search-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "ELASTICSEARCH_URL", "value": "http://localhost:9200"},
					},
				}},
			},
			// Module 8: monitoring-service
			{
				Kind: registry.KindNamespaceDependency,
				Data: &TestPayload{data: map[string]interface{}{
					"component": "company/monitoring-service",
					"parameters": []interface{}{
						map[string]interface{}{"name": "PROMETHEUS_URL", "value": "http://localhost:9090"},
						map[string]interface{}{"name": "GRAFANA_URL", "value": "http://localhost:3000"},
					},
				}},
			},
		}

		// Create load result with 8 loaded modules
		loadResult := &LoadResult{Modules: []LoadedModule{
			{Name: Name{Organization: "company", Module: "web-service"}},
			{Name: Name{Organization: "company", Module: "auth-service"}},
			{Name: Name{Organization: "company", Module: "payment-service"}},
			{Name: Name{Organization: "company", Module: "notification-service"}},
			{Name: Name{Organization: "company", Module: "analytics-service"}},
			{Name: Name{Organization: "company", Module: "file-service"}},
			{Name: Name{Organization: "company", Module: "search-service"}},
			{Name: Name{Organization: "company", Module: "monitoring-service"}},
		}}

		// Create parent dependency map
		parentMap := CreateParentDependencyMap(entries, loadResult, logger)

		// Verify all modules are mapped
		expectedModules := []string{
			"company/web-service",
			"company/auth-service",
			"company/payment-service",
			"company/notification-service",
			"company/analytics-service",
			"company/file-service",
			"company/search-service",
			"company/monitoring-service",
		}

		for _, module := range expectedModules {
			assert.Contains(t, parentMap, module, "Module %s should be in parent map", module)
		}

		// Verify specific mappings
		assert.Len(t, parentMap["company/web-service"], 2, "web-service should have 2 dependencies")
		assert.Len(t, parentMap["company/payment-service"], 2, "payment-service should have 2 dependencies")
		assert.Len(t, parentMap["company/auth-service"], 1, "auth-service should have 1 dependency")

		// Verify parameters are correctly extracted
		webServiceDeps := parentMap["company/web-service"]
		apiRouterFound := false
		databaseURLFound := false
		cacheURLFound := false

		for _, dep := range webServiceDeps {
			if dep.EntryID == "app:web_service_dep" {
				assert.Equal(t, "http://localhost:8080", dep.Parameters["API_ROUTER"])
				assert.Equal(t, "postgres://localhost:5432/webdb", dep.Parameters["DATABASE_URL"])
				apiRouterFound = true
				databaseURLFound = true
			}
			if dep.EntryID == "app:web_service_alt_dep" {
				assert.Equal(t, "redis://localhost:6379", dep.Parameters["CACHE_URL"])
				cacheURLFound = true
			}
		}

		assert.True(t, apiRouterFound, "API_ROUTER parameter should be found")
		assert.True(t, databaseURLFound, "DATABASE_URL parameter should be found")
		assert.True(t, cacheURLFound, "CACHE_URL parameter should be found")

		// Test requirement selection with 5 different requirements
		requirements := []registry.Entry{
			{ID: registry.ParseID("app:API_ROUTER"), Kind: registry.KindNamespaceRequirement},
			{ID: registry.ParseID("app:JWT_SECRET"), Kind: registry.KindNamespaceRequirement},
			{ID: registry.ParseID("app:STRIPE_KEY"), Kind: registry.KindNamespaceRequirement},
			{ID: registry.ParseID("app:ELASTICSEARCH_URL"), Kind: registry.KindNamespaceRequirement},
			{ID: registry.ParseID("app:PROMETHEUS_URL"), Kind: registry.KindNamespaceRequirement},
		}

		// Test API_ROUTER requirement (should match web_service_dep)
		apiRouterParent := SelectBestParentDependency(requirements[0], parentMap["company/web-service"], logger)
		assert.Equal(t, "app:web_service_dep", apiRouterParent, "API_ROUTER should match web_service_dep")

		// Test JWT_SECRET requirement (should match auth_service_dep)
		jwtSecretParent := SelectBestParentDependency(requirements[1], parentMap["company/auth-service"], logger)
		assert.Equal(t, "app:auth_service_dep", jwtSecretParent, "JWT_SECRET should match auth_service_dep")

		// Test STRIPE_KEY requirement (should match payment_service_dep)
		stripeKeyParent := SelectBestParentDependency(requirements[2], parentMap["company/payment-service"], logger)
		assert.Equal(t, "app:payment_service_dep", stripeKeyParent, "STRIPE_KEY should match payment_service_dep")

		// Test ELASTICSEARCH_URL requirement (should match search_service_dep)
		elasticsearchParent := SelectBestParentDependency(requirements[3], parentMap["company/search-service"], logger)
		assert.Equal(t, "app:search_service_dep", elasticsearchParent, "ELASTICSEARCH_URL should match search_service_dep")

		// Test PROMETHEUS_URL requirement (should match monitoring_service_dep)
		prometheusParent := SelectBestParentDependency(requirements[4], parentMap["company/monitoring-service"], logger)
		assert.Equal(t, "app:monitoring_service_dep", prometheusParent, "PROMETHEUS_URL should match monitoring_service_dep")

		// Test conflict validation - should pass since no conflicts exist
		err := ValidateParentDependencyConflicts(parentMap, logger)
		assert.NoError(t, err, "No conflicts should be detected")

		// Test a requirement that doesn't match any parameter (should fallback to first dependency)
		unknownRequirement := registry.Entry{ID: registry.ParseID("app:UNKNOWN_PARAM"), Kind: registry.KindNamespaceRequirement}
		unknownParent := SelectBestParentDependency(unknownRequirement, parentMap["company/web-service"], logger)
		assert.Equal(t, "app:web_service_dep", unknownParent, "Unknown parameter should fallback to first dependency")

		// Verify total count of dependencies
		totalDependencies := 0
		for _, deps := range parentMap {
			totalDependencies += len(deps)
		}
		assert.Equal(t, 10, totalDependencies, "Should have exactly 10 total dependencies")

		// Verify all expected parameters are present
		allParameters := make(map[string]string)
		for _, deps := range parentMap {
			for _, dep := range deps {
				for paramName, paramValue := range dep.Parameters {
					allParameters[paramName] = paramValue
				}
			}
		}

		expectedParameters := map[string]string{
			"API_ROUTER":        "http://localhost:8080",
			"DATABASE_URL":      "postgres://localhost:5432/webdb",
			"CACHE_URL":         "redis://localhost:6379",
			"JWT_SECRET":        "super-secret-key",
			"AUTH_DB":           "postgres://localhost:5432/authdb",
			"STRIPE_KEY":        "sk_test_123456789",
			"PAYMENT_DB":        "postgres://localhost:5432/paymentdb",
			"WEBHOOK_URL":       "https://api.company.com/webhooks",
			"SMTP_HOST":         "smtp.company.com",
			"SMS_API_KEY":       "sms_api_key_123",
			"ANALYTICS_DB":      "postgres://localhost:5432/analyticsdb",
			"KAFKA_BROKERS":     "localhost:9092",
			"S3_BUCKET":         "company-files",
			"S3_REGION":         "us-east-1",
			"ELASTICSEARCH_URL": "http://localhost:9200",
			"PROMETHEUS_URL":    "http://localhost:9090",
			"GRAFANA_URL":       "http://localhost:3000",
		}

		for paramName, expectedValue := range expectedParameters {
			actualValue, exists := allParameters[paramName]
			assert.True(t, exists, "Parameter %s should exist", paramName)
			assert.Equal(t, expectedValue, actualValue, "Parameter %s should have correct value", paramName)
		}

		assert.Equal(t, len(expectedParameters), len(allParameters), "Should have exactly %d parameters", len(expectedParameters))
	})
}
