package policy

import (
	"fmt"
	"github.com/ponyruntime/pony/api/service/policy"
	"regexp"
	"strconv"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
)

// ConditionEvaluator evaluates policy conditions against actors and metadata
type ConditionEvaluator struct{}

// NewConditionEvaluator creates a new ConditionEvaluator
func NewConditionEvaluator() *ConditionEvaluator {
	return &ConditionEvaluator{}
}

// EvaluateCondition evaluates a single condition
func (e *ConditionEvaluator) EvaluateCondition(
	condition policy.Condition,
	actor security.Actor,
	action, resource string,
	meta registry.Metadata,
) (bool, error) {
	// Extract the field value using dot notation
	fieldValue, err := e.extractField(condition.Field, actor, action, resource, meta)
	if err != nil {
		return false, err
	}

	// If value_from is specified, extract the comparison value
	var compareValue any
	if condition.ValueFrom != "" {
		compareValue, err = e.extractField(condition.ValueFrom, actor, action, resource, meta)
		if err != nil {
			return false, err
		}
	} else {
		compareValue = condition.Value
	}

	// Perform the comparison
	return e.compare(fieldValue, compareValue, condition.Operator)
}

// extractField extracts a value using dot notation from actor, metadata, or context
func (e *ConditionEvaluator) extractField(
	fieldPath string,
	actor security.Actor,
	action, resource string,
	meta registry.Metadata,
) (any, error) {
	parts := strings.Split(fieldPath, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty field path")
	}

	// Handle special cases
	switch parts[0] {
	case "actor":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid actor field path: %s", fieldPath)
		}
		return e.extractActorField(actor, parts[1:])
	case "meta":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid meta field path: %s", fieldPath)
		}
		return e.extractMetaField(meta, parts[1:])
	case "action":
		return action, nil
	case "resource":
		return resource, nil
	default:
		// Direct metadata key
		return meta[fieldPath], nil
	}
}

// extractActorField extracts a field from the actor
func (e *ConditionEvaluator) extractActorField(actor security.Actor, parts []string) (any, error) {
	if actor == nil {
		return nil, fmt.Errorf("actor is nil")
	}

	// Handle first level of actor fields
	if len(parts) == 0 {
		return nil, fmt.Errorf("no actor field specified")
	}

	switch parts[0] {
	case "id":
		return actor.ID(), nil
	case "meta":
		if len(parts) == 1 {
			return actor.Meta(), nil
		}

		// Handle nested metadata access
		actorMeta := actor.Meta()
		key := parts[1]
		if len(parts) == 2 {
			return actorMeta[key], nil
		}

		// Handle deeply nested access
		if nestedMap, ok := actorMeta[key].(map[string]any); ok {
			return e.extractNestedMap(nestedMap, parts[2:])
		}

		return nil, nil // Key not found or not a map
	default:
		return nil, fmt.Errorf("unknown actor field: %s", parts[0])
	}
}

// extractMetaField extracts a field from metadata
func (e *ConditionEvaluator) extractMetaField(meta registry.Metadata, parts []string) (any, error) {
	if meta == nil {
		return nil, nil // Return nil for non-existent metadata
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("no metadata field specified")
	}

	// If it's a direct metadata key
	key := parts[0]
	if len(parts) == 1 {
		return meta[key], nil
	}

	// Handle nested maps
	if nestedMap, ok := meta[key].(map[string]any); ok {
		return e.extractNestedMap(nestedMap, parts[1:])
	}

	return nil, nil // Key not found or not a map
}

// extractNestedMap handles nested map access without reflection
func (e *ConditionEvaluator) extractNestedMap(m map[string]any, parts []string) (any, error) {
	if len(parts) == 0 || m == nil {
		return m, nil
	}

	key := parts[0]
	value, exists := m[key]
	if !exists {
		return nil, nil
	}

	if len(parts) == 1 {
		return value, nil
	}

	// Continue traversing if the value is another map
	if nestedMap, ok := value.(map[string]any); ok {
		return e.extractNestedMap(nestedMap, parts[1:])
	}

	return nil, nil // Cannot traverse further
}

// compare performs the actual comparison based on the operator
func (e *ConditionEvaluator) compare(fieldValue, compareValue any, operator string) (bool, error) {
	// Handle nil values
	if fieldValue == nil {
		// Only exists operator can return true for nil field values
		return operator == "exists" && compareValue.(bool) == false, nil
	}

	// Handle special operators
	switch operator {
	case "exists":
		exists := fieldValue != nil
		if boolValue, ok := compareValue.(bool); ok {
			return exists == boolValue, nil
		}
		return exists, nil

	case "eq":
		return e.equals(fieldValue, compareValue)

	case "ne":
		result, err := e.equals(fieldValue, compareValue)
		return !result, err

	case "lt", "gt", "lte", "gte":
		return e.compareNumeric(fieldValue, compareValue, operator)

	case "in":
		return e.isIn(fieldValue, compareValue)

	case "contains":
		return e.contains(fieldValue, compareValue)

	case "matches":
		return e.matches(fieldValue, compareValue)

	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

// equals checks if fieldValue equals compareValue
func (e *ConditionEvaluator) equals(fieldValue, compareValue any) (bool, error) {
	// Direct equality check
	if fieldValue == compareValue {
		return true, nil
	}

	// Try type conversion for numeric comparisons
	fieldNum, fieldOk := e.toFloat64(fieldValue)
	compareNum, compareOk := e.toFloat64(compareValue)
	if fieldOk && compareOk {
		return fieldNum == compareNum, nil
	}

	// Try string comparison
	fieldStr, fieldOk := toString(fieldValue)
	compareStr, compareOk := toString(compareValue)
	if fieldOk && compareOk {
		return fieldStr == compareStr, nil
	}

	return false, nil
}

// compareNumeric handles numeric comparisons (lt, gt, lte, gte)
func (e *ConditionEvaluator) compareNumeric(fieldValue, compareValue any, operator string) (bool, error) {
	// Convert values to float64 for comparison
	fieldNum, fieldOk := e.toFloat64(fieldValue)
	compareNum, compareOk := e.toFloat64(compareValue)
	if !fieldOk || !compareOk {
		return false, fmt.Errorf("numeric comparison requires numeric values")
	}

	switch operator {
	case "lt":
		return fieldNum < compareNum, nil
	case "gt":
		return fieldNum > compareNum, nil
	case "lte":
		return fieldNum <= compareNum, nil
	case "gte":
		return fieldNum >= compareNum, nil
	default:
		return false, fmt.Errorf("unknown numeric operator: %s", operator)
	}
}

// isIn checks if fieldValue is in the compareValue (which should be a slice or array)
func (e *ConditionEvaluator) isIn(fieldValue, compareValue any) (bool, error) {
	// Convert compareValue to a slice if needed
	var slice []any

	switch cv := compareValue.(type) {
	case []any:
		slice = cv
	case []string:
		// Convert []string to []any
		slice = make([]any, len(cv))
		for i, v := range cv {
			slice[i] = v
		}
	case []int:
		// Convert []int to []any
		slice = make([]any, len(cv))
		for i, v := range cv {
			slice[i] = v
		}
	case string:
		// Single value comparison
		equal, _ := e.equals(fieldValue, cv)
		return equal, nil
	default:
		return false, fmt.Errorf("'in' operator requires slice or array for comparison")
	}

	// Check each element in the slice
	for _, item := range slice {
		equal, _ := e.equals(fieldValue, item)
		if equal {
			return true, nil
		}
	}

	return false, nil
}

// contains checks if fieldValue (which should be a string or slice) contains compareValue
func (e *ConditionEvaluator) contains(fieldValue, compareValue any) (bool, error) {
	// Handle string contains
	fieldStr, isFieldStr := toString(fieldValue)
	compareStr, isCompareStr := toString(compareValue)
	if isFieldStr && isCompareStr {
		return strings.Contains(fieldStr, compareStr), nil
	}

	// Handle slice contains
	if slice, ok := toSlice(fieldValue); ok {
		for _, item := range slice {
			equal, _ := e.equals(item, compareValue)
			if equal {
				return true, nil
			}
		}
		return false, nil
	}

	return false, fmt.Errorf("'contains' operator requires string or slice field value")
}

// matches checks if fieldValue (which should be a string) matches the regex pattern in compareValue
func (e *ConditionEvaluator) matches(fieldValue, compareValue any) (bool, error) {
	fieldStr, isFieldStr := toString(fieldValue)
	patternStr, isPatternStr := toString(compareValue)
	if !isFieldStr || !isPatternStr {
		return false, fmt.Errorf("'matches' operator requires string values")
	}

	// Compile and match regex
	pattern, err := regexp.Compile(patternStr)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return pattern.MatchString(fieldStr), nil
}

// toFloat64 attempts to convert a value to float64
func (e *ConditionEvaluator) toFloat64(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}

	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}

	return 0, false
}

// toString attempts to convert a value to string
func toString(value any) (string, bool) {
	if value == nil {
		return "", false
	}

	switch v := value.(type) {
	case string:
		return v, true
	case int:
		return strconv.Itoa(v), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	}

	// Try standard string conversion as last resort
	return fmt.Sprintf("%v", value), true
}

// toSlice attempts to convert a value to a slice
func toSlice(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}

	switch v := value.(type) {
	case []any:
		return v, true
	case []string:
		result := make([]any, len(v))
		for i, s := range v {
			result[i] = s
		}
		return result, true
	case []int:
		result := make([]any, len(v))
		for i, n := range v {
			result[i] = n
		}
		return result, true
	case string:
		// For strings, consider as array of runes/chars
		runes := []rune(v)
		result := make([]any, len(runes))
		for i, r := range runes {
			result[i] = string(r)
		}
		return result, true
	}

	return nil, false
}
