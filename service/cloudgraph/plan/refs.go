// SPDX-License-Identifier: MPL-2.0

package plan

import (
	"encoding/json"
	"fmt"
	"sort"
)

// OutputRef is a declared reference to another resource's output
// (design §8.4): serialized as {"$ref":{"resource_id":...,"output_key":...}}.
type OutputRef struct {
	ResourceID string `json:"resource_id"`
	OutputKey  string `json:"output_key"`
}

// Refs scans a desired document recursively and returns every $ref,
// deduplicated and deterministically ordered.
func Refs(desired json.RawMessage) ([]OutputRef, error) {
	if len(desired) == 0 {
		return nil, nil
	}

	var doc any
	if err := json.Unmarshal(desired, &doc); err != nil {
		return nil, fmt.Errorf("desired is not valid JSON: %w", err)
	}

	seen := make(map[OutputRef]struct{})
	if err := walkRefs(doc, seen); err != nil {
		return nil, err
	}

	refs := make([]OutputRef, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ResourceID != refs[j].ResourceID {
			return refs[i].ResourceID < refs[j].ResourceID
		}
		return refs[i].OutputKey < refs[j].OutputKey
	})
	return refs, nil
}

func walkRefs(node any, seen map[OutputRef]struct{}) error {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok, err := parseRef(v); err != nil {
			return err
		} else if ok {
			seen[ref] = struct{}{}
			return nil
		}
		for _, child := range v {
			if err := walkRefs(child, seen); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := walkRefs(child, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseRef recognizes a {"$ref": {...}} object. A $ref key with any other
// shape is a declaration error, never silently skipped.
func parseRef(node map[string]any) (OutputRef, bool, error) {
	raw, ok := node["$ref"]
	if !ok {
		return OutputRef{}, false, nil
	}
	body, ok := raw.(map[string]any)
	if !ok {
		return OutputRef{}, false, fmt.Errorf("$ref must be an object")
	}
	resourceID, _ := body["resource_id"].(string)
	outputKey, _ := body["output_key"].(string)
	if resourceID == "" || outputKey == "" {
		return OutputRef{}, false, fmt.Errorf("$ref requires resource_id and output_key")
	}
	return OutputRef{ResourceID: resourceID, OutputKey: outputKey}, true, nil
}

// Materialize replaces every $ref in a desired document with the value found
// in resolve, returning the rewritten document. resolve receives the ref and
// returns the replacement value (design §9.4 step 6).
func Materialize(desired json.RawMessage, resolve func(OutputRef) (any, error)) (json.RawMessage, error) {
	if len(desired) == 0 {
		return desired, nil
	}

	var doc any
	if err := json.Unmarshal(desired, &doc); err != nil {
		return nil, fmt.Errorf("desired is not valid JSON: %w", err)
	}

	rewritten, err := rewriteRefs(doc, resolve)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rewritten)
}

func rewriteRefs(node any, resolve func(OutputRef) (any, error)) (any, error) {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok, err := parseRef(v); err != nil {
			return nil, err
		} else if ok {
			return resolve(ref)
		}
		out := make(map[string]any, len(v))
		for key, child := range v {
			replaced, err := rewriteRefs(child, resolve)
			if err != nil {
				return nil, err
			}
			out[key] = replaced
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			replaced, err := rewriteRefs(child, resolve)
			if err != nil {
				return nil, err
			}
			out[i] = replaced
		}
		return out, nil
	default:
		return node, nil
	}
}
