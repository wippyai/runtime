package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/service/policy"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

type ConditionEvaluator struct {
	compiledPatterns map[string]*regexp.Regexp
}

func NewConditionEvaluator(conditions []policy.Condition) (*ConditionEvaluator, error) {
	patterns := make(map[string]*regexp.Regexp)

	for _, condition := range conditions {
		if condition.Operator == "matches" || condition.Operator == "nmatches" {
			if patternStr, ok := condition.Value.(string); ok {
				if _, exists := patterns[patternStr]; !exists {
					compiled, err := regexp.Compile(patternStr)
					if err != nil {
						return nil, fmt.Errorf("invalid regex pattern %q: %w", patternStr, err)
					}
					patterns[patternStr] = compiled
				}
			}
		}
	}

	return &ConditionEvaluator{compiledPatterns: patterns}, nil
}

func (e *ConditionEvaluator) EvaluateCondition(
	condition policy.Condition,
	actor security.Actor,
	action, resource string,
	meta registry.Metadata,
) (bool, error) {
	fieldValue, err := e.extractField(condition.Field, actor, action, resource, meta)
	if err != nil {
		return false, err
	}

	var compareValue any
	if condition.ValueFrom != "" {
		compareValue, err = e.extractField(condition.ValueFrom, actor, action, resource, meta)
		if err != nil {
			return false, err
		}
	} else {
		compareValue = condition.Value
	}

	return e.compare(fieldValue, compareValue, condition.Operator)
}

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
		return meta[fieldPath], nil
	}
}

func (e *ConditionEvaluator) extractActorField(actor security.Actor, parts []string) (any, error) {
	if actor.ID == "" && actor.Meta == nil {
		return nil, fmt.Errorf("nil or empty actor")
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("no actor field specified")
	}

	switch parts[0] {
	case "id":
		return actor.ID, nil
	case "meta":
		if len(parts) == 1 {
			return actor.Meta, nil
		}

		actorMeta := actor.Meta
		if actorMeta == nil {
			return nil, nil
		}

		key := parts[1]
		if len(parts) == 2 {
			return actorMeta[key], nil
		}

		if nestedMap, ok := actorMeta[key].(map[string]any); ok {
			return e.extractNestedMap(nestedMap, parts[2:])
		}

		return nil, nil
	default:
		return nil, fmt.Errorf("unknown actor field: %s", parts[0])
	}
}

func (e *ConditionEvaluator) extractMetaField(meta registry.Metadata, parts []string) (any, error) {
	if meta == nil {
		return nil, nil
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("no metadata field specified")
	}

	key := parts[0]
	if len(parts) == 1 {
		return meta[key], nil
	}

	if nestedMap, ok := meta[key].(map[string]any); ok {
		return e.extractNestedMap(nestedMap, parts[1:])
	}

	return nil, nil
}

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

	if nestedMap, ok := value.(map[string]any); ok {
		return e.extractNestedMap(nestedMap, parts[1:])
	}

	return nil, nil
}

func (e *ConditionEvaluator) compare(fieldValue, compareValue any, operator string) (bool, error) {
	if fieldValue == nil {
		switch operator {
		case "exists":
			return !compareValue.(bool), nil
		case "nexists":
			return compareValue.(bool), nil
		default:
			return false, nil
		}
	}

	switch operator {
	case "exists":
		if boolValue, ok := compareValue.(bool); ok {
			return boolValue, nil
		}
		return true, nil

	case "nexists":
		if boolValue, ok := compareValue.(bool); ok {
			return !boolValue, nil
		}
		return false, nil

	case "eq":
		return e.equals(fieldValue, compareValue)

	case "ne":
		result, err := e.equals(fieldValue, compareValue)
		return !result, err

	case "lt", "gt", "lte", "gte":
		return e.compareNumeric(fieldValue, compareValue, operator)

	case "in":
		return e.isIn(fieldValue, compareValue)

	case "nin":
		result, err := e.isIn(fieldValue, compareValue)
		return !result, err

	case "contains":
		return e.contains(fieldValue, compareValue)

	case "ncontains":
		result, err := e.contains(fieldValue, compareValue)
		return !result, err

	case "matches":
		return e.matches(fieldValue, compareValue)

	case "nmatches":
		result, err := e.matches(fieldValue, compareValue)
		return !result, err

	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func (e *ConditionEvaluator) equals(fieldValue, compareValue any) (bool, error) {
	if fieldValue == compareValue {
		return true, nil
	}

	fieldNum, fieldOk := e.toFloat64(fieldValue)
	compareNum, compareOk := e.toFloat64(compareValue)
	if fieldOk && compareOk {
		return fieldNum == compareNum, nil
	}

	fieldStr, fieldOk := toString(fieldValue)
	compareStr, compareOk := toString(compareValue)
	if fieldOk && compareOk {
		return fieldStr == compareStr, nil
	}

	return false, nil
}

func (e *ConditionEvaluator) compareNumeric(fieldValue, compareValue any, operator string) (bool, error) {
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

func (e *ConditionEvaluator) isIn(fieldValue, compareValue any) (bool, error) {
	var slice []any

	switch cv := compareValue.(type) {
	case []any:
		slice = cv
	case []string:
		slice = make([]any, len(cv))
		for i, v := range cv {
			slice[i] = v
		}
	case []int:
		slice = make([]any, len(cv))
		for i, v := range cv {
			slice[i] = v
		}
	case string:
		equal, _ := e.equals(fieldValue, cv)
		return equal, nil
	default:
		return false, fmt.Errorf("'in' operator requires slice or array for comparison")
	}

	for _, item := range slice {
		equal, _ := e.equals(fieldValue, item)
		if equal {
			return true, nil
		}
	}

	return false, nil
}

func (e *ConditionEvaluator) contains(fieldValue, compareValue any) (bool, error) {
	fieldStr, isFieldStr := toString(fieldValue)
	compareStr, isCompareStr := toString(compareValue)
	if isFieldStr && isCompareStr {
		return strings.Contains(fieldStr, compareStr), nil
	}

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

func (e *ConditionEvaluator) matches(fieldValue, compareValue any) (bool, error) {
	fieldStr, isFieldStr := toString(fieldValue)
	patternStr, isPatternStr := toString(compareValue)
	if !isFieldStr || !isPatternStr {
		return false, fmt.Errorf("'matches' operator requires string values")
	}

	pattern, exists := e.compiledPatterns[patternStr]
	if !exists {
		return false, fmt.Errorf("regex pattern %q not pre-compiled", patternStr)
	}

	return pattern.MatchString(fieldStr), nil
}

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

	return fmt.Sprintf("%v", value), true
}

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
		runes := []rune(v)
		result := make([]any, len(runes))
		for i, r := range runes {
			result[i] = string(r)
		}
		return result, true
	}

	return nil, false
}
