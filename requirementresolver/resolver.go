package requirementresolver

import (
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"

	"github.com/ponyruntime/pony/system/registry/loader"

	"github.com/ponyruntime/pony/api/registry"
)

// this function resolves dependencies for modules
// 	// KindNamespaceDefinition represents namespace definition variable which can be declared by export or any other source.
//	KindNamespaceDefinition Kind = "ns.definition"
//
//	// KindNamespaceDependency represents a module dependency entry
//	KindNamespaceDependency Kind = "ns.dependency"
//
//	// KindNamespaceRequirement represents a module requirement entry
//	KindNamespaceRequirement Kind = "ns.requirement"

func ResolveModuleRequirements(entries []registry.Entry) error {
	// 1. build map of ns.definition
	// 2. build map of ns.dependency
	// 3. build map of ns.requirement

	nsDefinitions := make(map[string]registry.Entry)
	nsDependencies := make(map[string]registry.Entry)
	nsRequirements := make(map[string]registry.Entry)

	for _, entry := range entries {
		switch entry.Kind {
		case registry.KindNamespaceDefinition:
			nsDefinitions[entry.ID.Name] = entry
		case registry.KindNamespaceDependency:
			nsDependencies[entry.ID.Name] = entry
		case registry.KindNamespaceRequirement:
			nsRequirements[entry.ID.Name] = entry
		}
	}

	log.Printf("nsDefinitions: %s", spew.Sdump(nsDefinitions))
	log.Printf("nsDependencies: %s", spew.Sdump(nsDependencies))
	log.Printf("nsRequirements: %s", spew.Sdump(nsRequirements))

	// for _, entry := range entries {
	// 	if strings.Contains(entry.ID.String(), "hello_endpoint") {
	// 		log.Printf("hello_endpoint entry: %s", spew.Sdump(entry))
	// 	}
	// }

	for _, nsRequirement := range nsRequirements {
		nsDependency, path, err := findRequirementDependency(nsRequirement, nsDependencies)
		if err != nil {
			log.Printf("warning: failed to find requirement dependency for %s: %v", nsRequirement.ID.Name, err)
			continue
		}

		log.Printf("nsDependency: %s", spew.Sdump(nsDependency))
		log.Printf("path: %s", path)

		value, err := getValueFromEntry(nsDependency, path)
		if err != nil {
			log.Printf("warning: failed to get value from entry %s for %s: %v", nsDependency.ID.Name, path, err)
		}

		nsDefinition, err := findRequirementDefinition(nsRequirement, nsDefinitions)
		if err != nil {
			log.Printf("warning: failed to find requirement definition for %s: %v", nsRequirement.ID.Name, err)
			continue
		}

		log.Printf("nsDefinition: %s", spew.Sdump(nsDefinition))

		definitionTargets, err := getDefinitionTargets(nsDefinition)
		if err != nil {
			log.Printf("warning: failed to get definition targets for %s: %v", nsDefinition.ID.Name, err)
			continue
		}

		for _, definitionTarget := range definitionTargets {
			log.Printf("definitionTarget: %s", spew.Sdump(definitionTarget))

			targetEntries, err := findDefinitionTargetEntries(definitionTarget, nsDefinition.ID.NS, entries)
			if err != nil {
				log.Printf("warning: failed to find definition target entries for %s: %v", definitionTarget.Name, err)
				continue
			}

			log.Printf("targetEntries: %s", spew.Sdump(targetEntries))
			err = applyPathValueToEntries(definitionTarget.Value, value, targetEntries)
			if err != nil {
				log.Printf("warning: failed to apply path value to entries for %s: %v", definitionTarget.Name, err)
				continue
			}
		}
	}

	return nil
}

func applyPathValueToEntries(targetPath string, value string, entries []registry.Entry) error {
	/*
			example of: targetPath, value and entry
			#1 this means we need to find "meta" field next inside of this object find the field "depends_on". [] means we need to push new value to the slice
			targetPath: meta.depends_on[]
			value: ns:system
			entry: multiple entries from namespace

			#2 this means we need to find "meta" field next inside of this object find the field "router" and set it to value
			targetPath: meta.router
			value: meta.router
			entry: locate hello_endpoint entry

			entry example:
			2025/07/01 21:04:30 hello_endpoint entry: (registry.Entry) {
		 ID: (registry.ID) localspace:hello_endpoint,
		 Kind: (string) (len=13) "http.endpoint",
		 Meta: (registry.Metadata) (len=1) {
		  (string) (len=7) "comment": (string) (len=42) "HTTP endpoint which executes hello_handler"
		 },
		 Data: (payload.payload) {
		  data: (map[string]interface {}) (len=6) {
		   (string) (len=6) "method": (string) (len=3) "GET",
		   (string) (len=4) "name": (string) (len=14) "hello_endpoint",
		   (string) (len=4) "path": (string) (len=12) "/local/hello",
		   (string) (len=4) "func": (string) (len=13) "hello_handler",
		   (string) (len=4) "kind": (string) (len=13) "http.endpoint",
		   (string) (len=4) "meta": (map[string]interface {}) (len=1) {
		    (string) (len=7) "comment": (string) (len=42) "HTTP endpoint which executes hello_handler"
		   }
		  },
		  format: (payload.Format) (len=10) "golang/any"
		 }
		}

	*/

	for i := range entries {
		entry := &entries[i]

		if strings.HasSuffix(targetPath, "[]") {
			basePath := strings.TrimSuffix(targetPath, "[]")
			err := setValueByPath(entry, basePath, value, true)
			if err != nil {
				log.Printf("warning: failed to append value at path %s for entry %s: %v", targetPath, entry.ID.Name, err)
				continue
			}

			log.Printf("updated entry 1: %s", spew.Sdump(entries[i]))
		} else {
			err := setValueByPath(entry, targetPath, value, false)
			if err != nil {
				log.Printf("warning: failed to set value at path %s for entry %s: %v", targetPath, entry.ID.Name, err)
				continue
			}

			log.Printf("updated entry 2: %s", spew.Sdump(entries[i]))
		}
	}
	return nil
}

// setValueByPath sets or appends a value at the given dynamic path in the entry, using JSON tags for struct field access
func setValueByPath(target interface{}, path string, value string, isAppend bool) error {
	parts := parsePath(path)
	if parts == nil || len(parts) == 0 {
		return fmt.Errorf("invalid path syntax")
	}
	return setNestedValueByPath(target, parts, value, isAppend)
}

// setNestedValueByPath traverses and sets a value using JSON tags for struct fields, and keys for maps
func setNestedValueByPath(target interface{}, parts []interface{}, value string, isAppend bool) error {
	if len(parts) == 0 {
		return nil
	}

	// Use a more sophisticated approach that can handle array filters in intermediate paths
	return setValueWithArrayFilters(target, parts, value, isAppend)
}

// setValueWithArrayFilters handles complex path traversal including array filters
func setValueWithArrayFilters(target interface{}, parts []interface{}, value string, isAppend bool) error {
	if len(parts) == 0 {
		return nil
	}

	// Convert target to a map[string]interface{} for easier manipulation
	var targetMap map[string]interface{}

	v := reflect.ValueOf(target)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() == reflect.Map {
		// Convert map to map[string]interface{}
		targetMap = make(map[string]interface{})
		for _, key := range v.MapKeys() {
			targetMap[key.String()] = v.MapIndex(key).Interface()
		}
	} else if v.Kind() == reflect.Struct {
		// Convert struct to map[string]interface{} using JSON tags
		targetMap = structToMap(v)
	} else {
		return fmt.Errorf("unsupported target type: %v", v.Kind())
	}

	// Traverse the path and modify the target map
	err := traverseAndModifyMap(targetMap, parts, value, isAppend)
	if err != nil {
		return err
	}

	// Update the original target with the modified map
	if v.Kind() == reflect.Map {
		// Clear the original map and set new values
		for _, key := range v.MapKeys() {
			v.SetMapIndex(key, reflect.Value{})
		}
		for k, val := range targetMap {
			v.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(val))
		}
	} else if v.Kind() == reflect.Struct {
		// Update struct fields from map
		mapToStruct(targetMap, v)
	}

	return nil
}

// structToMap converts a struct to map[string]interface{} using JSON tags
func structToMap(v reflect.Value) map[string]interface{} {
	result := make(map[string]interface{})
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			jsonTag = field.Name
		}
		// Remove comma and options from JSON tag
		if idx := strings.Index(jsonTag, ","); idx != -1 {
			jsonTag = jsonTag[:idx]
		}

		fieldValue := v.Field(i)
		if fieldValue.CanInterface() {
			result[jsonTag] = fieldValue.Interface()
		}
	}

	return result
}

// mapToStruct updates struct fields from a map
func mapToStruct(m map[string]interface{}, v reflect.Value) {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			jsonTag = field.Name
		}
		// Remove comma and options from JSON tag
		if idx := strings.Index(jsonTag, ","); idx != -1 {
			jsonTag = jsonTag[:idx]
		}

		if val, exists := m[jsonTag]; exists {
			fieldValue := v.Field(i)
			if fieldValue.CanSet() {
				setValueToField(fieldValue, val)
			}
		}
	}
}

// setValueToField sets a value to a struct field
func setValueToField(field reflect.Value, value interface{}) {
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	switch v := value.(type) {
	case string:
		if field.Kind() == reflect.String {
			field.SetString(v)
		}
	case int, int8, int16, int32, int64:
		if field.Kind() == reflect.Int || field.Kind() == reflect.Int8 || field.Kind() == reflect.Int16 || field.Kind() == reflect.Int32 || field.Kind() == reflect.Int64 {
			field.SetInt(reflect.ValueOf(v).Int())
		}
	case float32, float64:
		if field.Kind() == reflect.Float32 || field.Kind() == reflect.Float64 {
			field.SetFloat(reflect.ValueOf(v).Float())
		}
	case bool:
		if field.Kind() == reflect.Bool {
			field.SetBool(v)
		}
	case []interface{}:
		if field.Kind() == reflect.Slice {
			field.Set(reflect.ValueOf(v))
		}
	case map[string]interface{}:
		if field.Kind() == reflect.Map {
			field.Set(reflect.ValueOf(v))
		}
	}
}

// traverseAndModifyMap traverses a map and modifies it according to the path
func traverseAndModifyMap(target map[string]interface{}, parts []interface{}, value string, isAppend bool) error {
	if len(parts) == 0 {
		return nil
	}

	current := target

	// Traverse all but the last part
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]

		switch p := part.(type) {
		case string:
			// Simple string key
			if _, exists := current[p]; !exists {
				current[p] = make(map[string]interface{})
			}

			if nextMap, ok := current[p].(map[string]interface{}); ok {
				current = nextMap
			} else {
				return fmt.Errorf("cannot navigate to field '%s' - not a map", p)
			}

		case *arrayFilter:
			// Array filter - find the matching element
			if slice, ok := current["services"].([]interface{}); ok {
				filtered := make([]interface{}, 0)
				for _, item := range slice {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if matchesFilter(itemMap, p) {
							filtered = append(filtered, item)
						}
					}
				}

				if len(filtered) == 0 {
					return fmt.Errorf("no items match filter '%s'", p.String())
				}
				if len(filtered) > 1 {
					return fmt.Errorf("multiple items match filter '%s', expected exactly one", p.String())
				}

				if itemMap, ok := filtered[0].(map[string]interface{}); ok {
					current = itemMap
				} else {
					return fmt.Errorf("filtered item is not a map")
				}
			} else {
				return fmt.Errorf("cannot apply array filter - 'services' is not a slice")
			}

		default:
			return fmt.Errorf("invalid path part type: %T", part)
		}
	}

	// Set the value at the final part
	return setValueInMap(current, parts[len(parts)-1], value, isAppend)
}

// setValueInMap sets a value in a map according to the final path part
func setValueInMap(target map[string]interface{}, part interface{}, value string, isAppend bool) error {
	switch p := part.(type) {
	case string:
		if isAppend {
			// Append to array
			if existing, exists := target[p]; exists {
				if arr, ok := existing.([]interface{}); ok {
					target[p] = append(arr, value)
				} else {
					target[p] = []interface{}{value}
				}
			} else {
				target[p] = []interface{}{value}
			}
		} else {
			// Set simple value
			target[p] = value
		}
		return nil

	case *arrayFilter:
		// Array filter in final part - this is more complex
		// For now, we'll handle the common case where we want to set a field in a filtered item
		return fmt.Errorf("array filters in final part not yet supported")

	default:
		return fmt.Errorf("invalid final part type: %T", part)
	}
}

func findRequirementDependency(nsRequirement registry.Entry, nsDependencies map[string]registry.Entry) (registry.Entry, string, error) {
	/*
		2025/07/01 15:19:37 nsDependency: (registry.Entry) {
		 ID: (registry.ID) app.requirements.demo:hello_world_dependency,
		 Kind: (string) (len=13) "ns.dependency",
		 Meta: (registry.Metadata) (len=3) {
		  (string) (len=11) "description": (string) (len=44) "Component dependency management demo example",
		  (string) (len=7) "comment": (string) (len=46) "Requirements and Dependencies Demo Application",
		  (string) (len=10) "depends_on": ([]interface {}) (len=1 cap=1) {
		   (string) (len=9) "ns:system"
		  }
		 },
		 Data: (payload.payload) {
		  data: (map[string]interface {}) (len=7) {
		   (string) (len=10) "parameters": ([]interface {}) (len=2 cap=2) {
		    (map[string]interface {}) (len=2) {
		     (string) (len=4) "name": (string) (len=10) "api_router",
		     (string) (len=5) "value": (string) (len=10) "system:api"
		    },
		    (map[string]interface {}) (len=2) {
		     (string) (len=4) "name": (string) (len=4) "text",
		     (string) (len=5) "value": (string) (len=12) "Updated Text"
		    }
		   },
		   (string) (len=7) "version": (string) (len=8) ">=v0.0.1",
		   (string) (len=9) "component": (string) (len=18) "igor-test-3/test-2",
		   (string) (len=4) "kind": (string) (len=13) "ns.dependency",
		   (string) (len=4) "meta": (map[string]interface {}) (len=1) {
		    (string) (len=11) "description": (string) (len=44) "Component dependency management demo example"
		   },
		   (string) (len=4) "name": (string) (len=22) "hello_world_dependency",
		   (string) (len=9) "namespace": (string) (len=21) "app.requirements.demo"
		  },
		  format: (payload.Format) (len=10) "golang/any"
		 }
		}
	*/

	/*
		2025/07/01 15:19:37 nsRequirements: (map[string]registry.Entry) (len=3) {
		 (string) (len=9) "NAMESPACE": (registry.Entry) {
		  ID: (registry.ID) app.requirements.demo:NAMESPACE,
		  Kind: (string) (len=14) "ns.requirement",
		  Meta: (registry.Metadata) (len=2) {
		   (string) (len=7) "comment": (string) (len=46) "Requirements and Dependencies Demo Application",
		   (string) (len=10) "depends_on": ([]interface {}) (len=1 cap=1) {
		    (string) (len=9) "ns:system"
		   }
		  },
		  Data: (payload.payload) {
		   data: (map[string]interface {}) (len=3) {
		    (string) (len=4) "name": (string) (len=9) "NAMESPACE",
		    (string) (len=7) "targets": ([]interface {}) (len=1 cap=1) {
		     (map[string]interface {}) (len=2) {
		      (string) (len=5) "entry": (string) (len=22) "hello_world_dependency",
		      (string) (len=4) "path": (string) (len=9) "namespace"
		     }
		    },
		    (string) (len=4) "kind": (string) (len=14) "ns.requirement"
		   },
		   format: (payload.Format) (len=10) "golang/any"
		  }
		 },
		 (string) (len=10) "API_ROUTER": (registry.Entry) {
		  ID: (registry.ID) app.requirements.demo:API_ROUTER,
		  Kind: (string) (len=14) "ns.requirement",
		  Meta: (registry.Metadata) (len=2) {
		   (string) (len=7) "comment": (string) (len=46) "Requirements and Dependencies Demo Application",
		   (string) (len=10) "depends_on": ([]interface {}) (len=1 cap=1) {
		    (string) (len=9) "ns:system"
		   }
		  },
		  Data: (payload.payload) {
		   data: (map[string]interface {}) (len=3) {
		    (string) (len=4) "kind": (string) (len=14) "ns.requirement",
		    (string) (len=4) "name": (string) (len=10) "API_ROUTER",
		    (string) (len=7) "targets": ([]interface {}) (len=1 cap=1) {
		     (map[string]interface {}) (len=2) {
		      (string) (len=4) "path": (string) (len=33) "parameters[name=api_router].value",
		      (string) (len=5) "entry": (string) (len=22) "hello_world_dependency"
		     }
		    }
		   },
		   format: (payload.Format) (len=10) "golang/any"
		  }
		 },
		 (string) (len=4) "TEXT": (registry.Entry) {
		  ID: (registry.ID) app.requirements.demo:TEXT,
		  Kind: (string) (len=14) "ns.requirement",
		  Meta: (registry.Metadata) (len=2) {
		   (string) (len=7) "comment": (string) (len=46) "Requirements and Dependencies Demo Application",
		   (string) (len=10) "depends_on": ([]interface {}) (len=1 cap=1) {
		    (string) (len=9) "ns:system"
		   }
		  },
		  Data: (payload.payload) {
		   data: (map[string]interface {}) (len=3) {
		    (string) (len=4) "kind": (string) (len=14) "ns.requirement",
		    (string) (len=4) "name": (string) (len=4) "TEXT",
		    (string) (len=7) "targets": ([]interface {}) (len=1 cap=1) {
		     (map[string]interface {}) (len=2) {
		      (string) (len=4) "path": (string) (len=27) "parameters[name=text].value",
		      (string) (len=5) "entry": (string) (len=22) "hello_world_dependency"
		     }
		    }
		   },
		   format: (payload.Format) (len=10) "golang/any"
		  }
		 }
		}
	*/

	reqData := nsRequirement.Data.Data()
	reqMap, ok := reqData.(map[string]interface{})
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid requirement data in definition %s", nsRequirement.ID.Name)
	}

	targetsRaw, ok := reqMap["targets"].([]interface{})
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid requirement data in definition %s", nsRequirement.ID.Name)
	}

	// Iterate through all targets to find one that matches a dependency
	for _, targetRaw := range targetsRaw {
		if targetMap, ok := targetRaw.(map[string]interface{}); ok {
			// Check if the target entry matches any dependency name
			if entryName, ok := targetMap["entry"].(string); ok {
				for _, nsDependency := range nsDependencies {
					if entryName == nsDependency.ID.Name &&
						nsRequirement.ID.NS == nsDependency.ID.NS {
						// The target map has "path" field, not "value"
						if path, ok := targetMap["path"].(string); ok {
							return nsDependency, path, nil
						}
					}
				}
			}
		}
	}

	return registry.Entry{}, "", fmt.Errorf("dependency for requirement %s not found", nsRequirement.ID.Name)
}

func getValueFromEntry(entry registry.Entry, path string) (string, error) {
	/*
		path is a jq format path to the value in the entry

		path examples:
		namespace
		parameters[name=text].value
		parameters[name=api_router].value
		parameters[name=text].value

		for fields syntax is: key.key.key
		for slices of objects like parameters = []struct{Name string, Value string} syntax is: key[name=index].value
	*/

	/*
		2025/07/01 15:19:37 nsDependency: (registry.Entry) {
		 ID: (registry.ID) app.requirements.demo:hello_world_dependency,
		 Kind: (string) (len=13) "ns.dependency",
		 Meta: (registry.Metadata) (len=3) {
		  (string) (len=11) "description": (string) (len=44) "Component dependency management demo example",
		  (string) (len=7) "comment": (string) (len=46) "Requirements and Dependencies Demo Application",
		  (string) (len=10) "depends_on": ([]interface {}) (len=1 cap=1) {
		   (string) (len=9) "ns:system"
		  }
		 },
		 Data: (payload.payload) {
		  data: (map[string]interface {}) (len=7) {
		   (string) (len=10) "parameters": ([]interface {}) (len=2 cap=2) {
		    (map[string]interface {}) (len=2) {
		     (string) (len=4) "name": (string) (len=10) "api_router",
		     (string) (len=5) "value": (string) (len=10) "system:api"
		    },
		    (map[string]interface {}) (len=2) {
		     (string) (len=4) "name": (string) (len=4) "text",
		     (string) (len=5) "value": (string) (len=12) "Updated Text"
		    }
		   },
		   (string) (len=7) "version": (string) (len=8) ">=v0.0.1",
		   (string) (len=9) "component": (string) (len=18) "igor-test-3/test-2",
		   (string) (len=4) "kind": (string) (len=13) "ns.dependency",
		   (string) (len=4) "meta": (map[string]interface {}) (len=1) {
		    (string) (len=11) "description": (string) (len=44) "Component dependency management demo example"
		   },
		   (string) (len=4) "name": (string) (len=22) "hello_world_dependency",
		   (string) (len=9) "namespace": (string) (len=21) "app.requirements.demo"
		  },
		  format: (payload.Format) (len=10) "golang/any"
		 }
		}
	*/

	/*
		examples of values for path:
		parameters[name=text].value = "Updated Text"
		parameters[name=api_router].value = "system:api"
		namespace = "app.requirements.demo"
	*/

	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Get the data from the entry
	data := entry.Data.Data()
	if data == nil {
		return "", fmt.Errorf("entry data is nil")
	}

	// Parse the path and navigate to the target value
	value, err := navigatePath(data, path)
	if err != nil {
		return "", fmt.Errorf("failed to navigate path '%s': %w", path, err)
	}

	// Convert the value to string
	if value == nil {
		return "", nil
	}

	switch v := value.(type) {
	case string:
		return v, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32, float64:
		return fmt.Sprintf("%g", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// navigatePath navigates through the data structure using the jq-style path
func navigatePath(data interface{}, path string) (interface{}, error) {
	parts := parsePath(path)
	if parts == nil {
		return nil, fmt.Errorf("invalid path syntax")
	}
	current := data

	for i, part := range parts {
		isLast := i == len(parts)-1

		switch p := part.(type) {
		case string:
			// Simple field access
			if mapData, ok := current.(map[string]interface{}); ok {
				if value, exists := mapData[p]; exists {
					current = value
				} else {
					return nil, fmt.Errorf("field '%s' not found", p)
				}
			} else {
				return nil, fmt.Errorf("cannot access field '%s' on non-map type %T", p, current)
			}

		case *arrayFilter:
			// Array filtering with condition
			if sliceData, ok := current.([]interface{}); ok {
				filtered := make([]interface{}, 0)
				for _, item := range sliceData {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if matchesFilter(itemMap, p) {
							filtered = append(filtered, item)
						}
					}
				}

				if len(filtered) == 0 {
					return nil, fmt.Errorf("no items match filter '%s'", p.String())
				}

				if isLast {
					// If this is the last part and we have multiple matches, return the first one
					if len(filtered) > 1 {
						// For the last part, if we have multiple matches, we might want to return all
						// But for now, let's return the first match
						current = filtered[0]
					} else {
						current = filtered[0]
					}
				} else {
					// If not the last part, we expect exactly one match
					if len(filtered) > 1 {
						return nil, fmt.Errorf("multiple items match filter '%s', expected exactly one", p.String())
					}
					current = filtered[0]
				}
			} else {
				return nil, fmt.Errorf("cannot apply filter '%s' on non-slice type %T", p.String(), current)
			}

		default:
			return nil, fmt.Errorf("invalid path part type: %T", part)
		}
	}

	return current, nil
}

// parsePath parses a jq-style path into parts
func parsePath(path string) []interface{} {
	var parts []interface{}
	var current strings.Builder
	var inBracket bool
	var bracketContent strings.Builder

	for i := 0; i < len(path); i++ {
		char := path[i]

		switch char {
		case '.':
			if !inBracket {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				bracketContent.WriteByte(char)
			}

		case '[':
			if !inBracket {
				inBracket = true
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				bracketContent.WriteByte(char)
			}

		case ']':
			if inBracket {
				inBracket = false
				filter := parseArrayFilter(bracketContent.String())
				parts = append(parts, filter)
				bracketContent.Reset()
			} else {
				return nil // Invalid path - unmatched closing bracket
			}

		default:
			if inBracket {
				bracketContent.WriteByte(char)
			} else {
				current.WriteByte(char)
			}
		}
	}

	// Add the last part if there's anything left
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	// Check for unmatched opening bracket
	if inBracket {
		return nil // Invalid path - unmatched opening bracket
	}

	return parts
}

// arrayFilter represents a filter condition for array elements
type arrayFilter struct {
	field    string
	operator string
	value    string
}

func (af *arrayFilter) String() string {
	return fmt.Sprintf("[%s%s%s]", af.field, af.operator, af.value)
}

// parseArrayFilter parses array filter expressions like "name=text"
func parseArrayFilter(filter string) *arrayFilter {
	// Handle different operators: =, !=, >, <, >=, <=
	operators := []string{"!=", ">=", "<=", "=", ">", "<"}

	for _, op := range operators {
		if strings.Contains(filter, op) {
			parts := strings.SplitN(filter, op, 2)
			if len(parts) == 2 {
				return &arrayFilter{
					field:    strings.TrimSpace(parts[0]),
					operator: op,
					value:    strings.TrimSpace(parts[1]),
				}
			}
		}
	}

	// Default to equality if no operator found
	return &arrayFilter{
		field:    strings.TrimSpace(filter),
		operator: "=",
		value:    "",
	}
}

// matchesFilter checks if a map matches the given filter condition
func matchesFilter(item map[string]interface{}, filter *arrayFilter) bool {
	fieldValue, exists := item[filter.field]
	if !exists {
		return false
	}

	// Convert field value to string for comparison
	var fieldStr string
	switch v := fieldValue.(type) {
	case string:
		fieldStr = v
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		fieldStr = fmt.Sprintf("%d", v)
	case float32, float64:
		fieldStr = fmt.Sprintf("%g", v)
	case bool:
		fieldStr = fmt.Sprintf("%t", v)
	default:
		fieldStr = fmt.Sprintf("%v", v)
	}

	switch filter.operator {
	case "=":
		return fieldStr == filter.value
	case "!=":
		return fieldStr != filter.value
	case ">":
		return compareStrings(fieldStr, filter.value) > 0
	case "<":
		return compareStrings(fieldStr, filter.value) < 0
	case ">=":
		return compareStrings(fieldStr, filter.value) >= 0
	case "<=":
		return compareStrings(fieldStr, filter.value) <= 0
	default:
		return false
	}
}

// compareStrings compares two strings, trying to convert to numbers if possible
func compareStrings(a, b string) int {
	// Try to parse as numbers first
	if aNum, aErr := parseNumber(a); aErr == nil {
		if bNum, bErr := parseNumber(b); bErr == nil {
			if aNum < bNum {
				return -1
			} else if aNum > bNum {
				return 1
			}
			return 0
		}
	}

	// Fall back to string comparison
	return strings.Compare(a, b)
}

// parseNumber attempts to parse a string as a number
func parseNumber(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func findDefinitionTargetEntries(definitionTarget loader.RequirementTarget, ns string, entries []registry.Entry) ([]registry.Entry, error) {
	/*
	   	2025/06/30 23:14:25 definitionTarget: (loader.RequirementTarget) {
	       Name: (string) (len=14) "hello_endpoint",
	       Value: (string) (len=11) "meta.router"
	      }
	*/

	/*
		2025/06/30 23:14:25 definitionTarget: (loader.RequirementTarget) {
		 Name: (string) "",
		 Value: (string) (len=17) "meta.depends_on[]"
		}

	*/

	/*
			2025/07/01 20:56:01 found entry 0: (registry.Entry) {
		 ID: (registry.ID) localspace:hello_endpoint,
		 Kind: (string) (len=14) "ns.requirement",
	*/

	results := make([]registry.Entry, 0)

	for _, entry := range entries {
		if definitionTarget.Name != "" && strings.Contains(entry.ID.String(), definitionTarget.Name) {
			log.Println("found entry 0:", spew.Sdump(entry), spew.Sdump(definitionTarget), ns)
		}
		// Check if the entry ID matches the definition target name
		if entry.ID.NS == ns {
			if definitionTarget.Name == "" {
				// When Name is empty, match by Value field which contains path like "meta.depends_on[]"
				if definitionTarget.Value != "" {
					// For now, just add all entries in the namespace when Value is specified
					log.Println("found entry 1:", spew.Sdump(entry))
					results = append(results, entry)
				}
				continue
			}

			// When Name is specified, match by exact name
			if entry.ID.Name == definitionTarget.Name {
				log.Println("found entry 2:", spew.Sdump(entry))
				results = append(results, entry)
			}
		}
	}

	return results, nil
}

func getDefinitionTargets(definition registry.Entry) ([]loader.RequirementTarget, error) {
	data := definition.Data.Data()
	requirement, ok := data.(loader.Requirement)
	if !ok {
		return nil, fmt.Errorf("invalid requirement data in definition %s", definition.ID.Name)
	}

	return requirement.Targets, nil
}

func findRequirementDefinition(requirement registry.Entry, nsDefinitions map[string]registry.Entry) (registry.Entry, error) {
	definition, ok := nsDefinitions[requirement.ID.Name]
	if !ok {
		return registry.Entry{}, fmt.Errorf("definition for requirement %s not found", requirement.ID.Name)
	}

	return definition, nil
}

func findDependencyRequirements(nsDependency registry.Entry, nsRequirements map[string]registry.Entry) ([]registry.Entry, error) {
	matchingRequirements := make([]registry.Entry, 0)

	for _, nsRequirement := range nsRequirements {
		reqData := nsRequirement.Data.Data()

		// Try to parse as loader.Requirement first (existing format)
		if requirement, ok := reqData.(loader.Requirement); ok {
			// Check if requirement targets match the dependency
			for _, target := range requirement.Targets {
				// Check if the target entry name matches the dependency name
				// and if they're in the same namespace
				if target.Name == nsDependency.ID.Name &&
					nsRequirement.ID.NS == nsDependency.ID.NS {
					matchingRequirements = append(matchingRequirements, nsRequirement)
					break
				}
			}
			continue
		}

		// Try to parse as raw map data (format from comments)
		if reqMap, ok := reqData.(map[string]interface{}); ok {
			// Extract targets from the raw map format
			if targetsRaw, ok := reqMap["targets"].([]interface{}); ok {
				for _, targetRaw := range targetsRaw {
					if targetMap, ok := targetRaw.(map[string]interface{}); ok {
						// Check if the target entry matches the dependency name
						if entryName, ok := targetMap["entry"].(string); ok {
							if entryName == nsDependency.ID.Name &&
								nsRequirement.ID.NS == nsDependency.ID.NS {
								matchingRequirements = append(matchingRequirements, nsRequirement)
								break
							}
						}
					}
				}
			}
		}
	}

	return matchingRequirements, nil
}

type Result struct {
	ProviderEntry registry.Entry
	TargetEntry   registry.Entry
}
