// SPDX-License-Identifier: MPL-2.0

package json

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/kaptinlin/jsonschema"
	lua "github.com/wippyai/go-lua"
)

const maxValidationDetails = 50

// schemaLeaf is a single violated rule of a failed schema evaluation.
type schemaLeaf struct {
	instancePath string
	keyword      string
	message      string
	schemaPath   string
}

// wrapperKeywords summarize the outcome of sub-schemas; their violations are
// reported through the sub-results they wrap.
var wrapperKeywords = map[string]bool{
	"properties": true, "patternProperties": true, "additionalProperties": true,
	"unevaluatedProperties": true, "items": true, "prefixItems": true, "additionalItems": true,
	"unevaluatedItems": true, "propertyNames": true, "allOf": true, "anyOf": true, "oneOf": true,
	"$ref": true, "$dynamicRef": true, "then": true, "else": true, "dependentSchemas": true,
}

// conditionKeywords evaluate sub-schemas whose failure is not a violation.
var conditionKeywords = map[string]bool{
	"if": true, "not": true, "contains": true,
}

var propertyKeywords = map[string]bool{
	"additionalProperties": true, "unevaluatedProperties": true,
}

// collectSchemaLeaves flattens an evaluation result into its violated leaf rules.
func collectSchemaLeaves(result *jsonschema.EvaluationResult) []schemaLeaf {
	if result == nil {
		return nil
	}
	leaves := collectNodeLeaves(result, "", "#")
	return dedupeLeaves(dropMissingPropertyNoise(leaves))
}

func collectNodeLeaves(node *jsonschema.EvaluationResult, basePath, schemaBase string) []schemaLeaf {
	instancePath := basePath + node.InstanceLocation

	covered := make(map[string]bool)
	var leaves []schemaLeaf
	for _, child := range node.Details {
		if child == nil || child.Valid {
			continue
		}
		keyword := firstSegment(child.EvaluationPath)
		if conditionKeywords[keyword] {
			continue
		}
		childSchema := schemaBase + child.EvaluationPath
		if child.EvaluationPath == "" {
			childSchema = schemaBase + "/$ref"
			keyword = "$ref"
		}
		childLeaves := collectNodeLeaves(child, instancePath, childSchema)
		if propertyKeywords[keyword] && isFalseSchemaNode(child) {
			covered[keyword] = true
			leaves = append(leaves, schemaLeaf{
				instancePath: instancePath + child.InstanceLocation,
				keyword:      keyword,
				message:      "Additional property '" + lastSegment(child.InstanceLocation) + "' not allowed",
				schemaPath:   childSchema,
			})
			continue
		}
		if len(childLeaves) > 0 {
			covered[keyword] = true
			if keyword == "$ref" {
				covered["$dynamicRef"] = true
			}
			leaves = append(leaves, childLeaves...)
		}
	}

	for keyword, evalErr := range node.Errors {
		if wrapperKeywords[keyword] && covered[keyword] {
			continue
		}
		message := evalErr.Localize(nil)
		if keyword == "required" {
			for _, name := range missingProperties(evalErr) {
				leaves = append(leaves, schemaLeaf{
					instancePath: joinPointer(instancePath, name),
					keyword:      keyword,
					message:      message,
					schemaPath:   schemaBase + "/required",
				})
			}
			continue
		}
		if keyword == "dependentRequired" {
			leaves = append(leaves, dependentRequiredLeaves(evalErr, instancePath, schemaBase)...)
			continue
		}
		leaves = append(leaves, schemaLeaf{
			instancePath: instancePath,
			keyword:      keyword,
			message:      message,
			schemaPath:   schemaBase + "/" + keyword,
		})
	}
	return leaves
}

// dependentRequiredLeaves yields one leaf per missing dependent property.
func dependentRequiredLeaves(evalErr *jsonschema.EvaluationError, instancePath, schemaBase string) []schemaLeaf {
	raw, _ := evalErr.Params["missing_properties"].(string)
	var missing map[string][]string
	if err := json.Unmarshal([]byte(raw), &missing); err != nil || len(missing) == 0 {
		return []schemaLeaf{{
			instancePath: instancePath,
			keyword:      "dependentRequired",
			message:      evalErr.Localize(nil),
			schemaPath:   schemaBase + "/dependentRequired",
		}}
	}
	triggers := make([]string, 0, len(missing))
	for trigger := range missing {
		triggers = append(triggers, trigger)
	}
	slices.Sort(triggers)
	var leaves []schemaLeaf
	for _, trigger := range triggers {
		for _, name := range missing[trigger] {
			leaves = append(leaves, schemaLeaf{
				instancePath: joinPointer(instancePath, name),
				keyword:      "dependentRequired",
				message:      "Property '" + name + "' is required when '" + trigger + "' is present",
				schemaPath:   schemaBase + "/dependentRequired",
			})
		}
	}
	return leaves
}

func isFalseSchemaNode(node *jsonschema.EvaluationResult) bool {
	if len(node.Errors) != 1 || len(node.Details) > 0 {
		return false
	}
	_, ok := node.Errors["schema"]
	return ok
}

// missingProperties extracts the property names from a required error.
func missingProperties(evalErr *jsonschema.EvaluationError) []string {
	raw, _ := evalErr.Params["property"].(string)
	if raw == "" {
		raw, _ = evalErr.Params["properties"].(string)
	}
	var names []string
	for _, part := range strings.Split(raw, "', '") {
		name := strings.Trim(part, "'")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// dropMissingPropertyNoise removes errors raised by evaluating the sub-schema
// of a property that is absent; the missing property is already reported by
// its required leaf.
func dropMissingPropertyNoise(leaves []schemaLeaf) []schemaLeaf {
	var missing []string
	for _, l := range leaves {
		if l.keyword == "required" {
			missing = append(missing, l.instancePath)
		}
	}
	if len(missing) == 0 {
		return leaves
	}
	out := leaves[:0:0]
	for _, l := range leaves {
		if l.keyword != "required" && underAny(l.instancePath, missing) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func underAny(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func dedupeLeaves(leaves []schemaLeaf) []schemaLeaf {
	seen := make(map[schemaLeaf]bool, len(leaves))
	out := make([]schemaLeaf, 0, len(leaves))
	for _, l := range leaves {
		if seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

func leafRank(keyword string) int {
	switch keyword {
	case "required":
		return 0
	case "additionalProperties", "unevaluatedProperties":
		return 1
	case "enum", "const":
		return 2
	case "type":
		return 3
	default:
		return 4
	}
}

func pointerDepth(path string) int {
	return strings.Count(path, "/")
}

func sortLeaves(leaves []schemaLeaf) {
	slices.SortStableFunc(leaves, func(a, b schemaLeaf) int {
		return cmp.Or(
			cmp.Compare(leafRank(a.keyword), leafRank(b.keyword)),
			cmp.Compare(pointerDepth(b.instancePath), pointerDepth(a.instancePath)),
			cmp.Compare(a.instancePath, b.instancePath),
			cmp.Compare(a.schemaPath, b.schemaPath),
		)
	})
}

func (l schemaLeaf) displayPath() string {
	if l.instancePath == "" {
		return "/"
	}
	return l.instancePath
}

func (l schemaLeaf) String() string {
	return l.displayPath() + ": " + l.keyword + ": " + l.message
}

func firstSegment(path string) string {
	path = strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}

func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return unescapePointer(path[i+1:])
	}
	return path
}

func joinPointer(base, name string) string {
	name = strings.ReplaceAll(name, "~", "~0")
	name = strings.ReplaceAll(name, "/", "~1")
	return base + "/" + name
}

func unescapePointer(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

// validationFailure builds the INVALID error for a failed evaluation. The
// message names the most specific leaf; details carry the ordered leaf list.
func validationFailure(l *lua.LState, result *jsonschema.EvaluationResult) *lua.Error {
	leaves := collectSchemaLeaves(result)
	if len(leaves) == 0 {
		return lua.NewLuaError(l, "validation failed").WithKind(lua.Invalid).WithRetryable(false)
	}
	sortLeaves(leaves)

	listed := leaves
	if len(listed) > maxValidationDetails {
		listed = listed[:maxValidationDetails]
	}
	errs := make([]any, len(listed))
	for i, leaf := range listed {
		errs[i] = map[string]any{
			"instance_path": leaf.displayPath(),
			"keyword":       leaf.keyword,
			"message":       leaf.message,
			"schema_path":   leaf.schemaPath,
		}
	}

	return lua.NewLuaError(l, "validation failed: "+leaves[0].String()).
		WithKind(lua.Invalid).
		WithRetryable(false).
		WithDetails(map[string]any{"errors": errs, "total": len(leaves)})
}
