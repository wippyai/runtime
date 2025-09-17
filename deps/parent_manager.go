package deps

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// ParentDependencyInfo contains information about a parent dependency
type ParentDependencyInfo struct {
	EntryID    string
	Parameters map[string]string // parameter name -> parameter value
}

// CreateParentDependencyMap creates a map of module names to their parent dependency information
func CreateParentDependencyMap(entries []registry.Entry, loadResult *LoadResult, logger *zap.Logger) map[string][]ParentDependencyInfo {
	parentMap := make(map[string][]ParentDependencyInfo)

	if loadResult == nil {
		return parentMap
	}

	// Create a set of loaded module names for quick lookup
	loadedModuleNames := make(map[string]bool)
	for _, module := range loadResult.Modules {
		loadedModuleNames[module.Name.String()] = true
	}

	// Find ns.dependency entries that match loaded modules
	for _, entry := range entries {
		if entry.Kind != registry.KindNamespaceDependency {
			continue
		}

		componentStr, err := extractComponentFromDependency(entry)
		if err != nil {
			logger.Debug("failed to extract component from dependency",
				zap.String("entry_id", entry.ID.String()),
				zap.Error(err))
			continue
		}

		// Check if this component matches any loaded module
		if !loadedModuleNames[componentStr] {
			continue
		}

		// Extract parameters from the dependency entry
		parameters := extractParametersFromDependency(entry)

		parentInfo := ParentDependencyInfo{
			EntryID:    entry.ID.String(),
			Parameters: parameters,
		}

		parentMap[componentStr] = append(parentMap[componentStr], parentInfo)
		logger.Debug("mapped module to parent dependency",
			zap.String("module_name", componentStr),
			zap.String("parent_dependency_id", entry.ID.String()),
			zap.Any("parameters", parameters))
	}

	return parentMap
}

// extractComponentFromDependency extracts component from a ns.dependency entry
func extractComponentFromDependency(entry registry.Entry) (string, error) {
	if entry.Data == nil {
		return "", fmt.Errorf("entry data is nil")
	}

	data := entry.Data.Data()
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("entry data is not a map")
	}

	component, exists := dataMap["component"]
	if !exists {
		return "", fmt.Errorf("component field not found")
	}

	componentStr, ok := component.(string)
	if !ok {
		return "", fmt.Errorf("component is not a string")
	}

	return componentStr, nil
}

// extractParametersFromDependency extracts parameters from a ns.dependency entry
func extractParametersFromDependency(entry registry.Entry) map[string]string {
	parameters := make(map[string]string)

	if entry.Data == nil {
		return parameters
	}

	data := entry.Data.Data()
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return parameters
	}

	paramsRaw, exists := dataMap["parameters"]
	if !exists {
		return parameters
	}

	paramsArray, ok := paramsRaw.([]interface{})
	if !ok {
		return parameters
	}

	for _, paramRaw := range paramsArray {
		paramMap, ok := paramRaw.(map[string]interface{})
		if !ok {
			continue
		}

		paramName, ok := paramMap["name"].(string)
		if !ok {
			continue
		}

		paramValue, ok := paramMap["value"].(string)
		if !ok {
			continue
		}

		parameters[paramName] = paramValue
	}

	return parameters
}

// SelectBestParentDependency selects the best parent dependency for a requirement based on parameter matching
func SelectBestParentDependency(requirement registry.Entry, parentDependencies []ParentDependencyInfo, logger *zap.Logger) string {
	if len(parentDependencies) == 0 {
		return ""
	}
	if len(parentDependencies) == 1 {
		return parentDependencies[0].EntryID
	}

	requirementName := requirement.ID.Name
	for _, parentDep := range parentDependencies {
		if _, hasParameter := parentDep.Parameters[requirementName]; hasParameter {
			logger.Debug("selected parent dependency based on parameter match",
				zap.String("requirement_name", requirementName),
				zap.String("parent_dependency_id", parentDep.EntryID),
				zap.String("parameter_value", parentDep.Parameters[requirementName]))
			return parentDep.EntryID
		}
	}

	// Fallback to first available parent dependency
	if len(parentDependencies) > 0 {
		logger.Debug("no parameter match found, using first available parent dependency",
			zap.String("requirement_name", requirementName),
			zap.String("selected_parent_dependency_id", parentDependencies[0].EntryID))
		return parentDependencies[0].EntryID
	}

	return ""
}

// ValidateParentDependencyConflicts validates that there are no conflicts in parent dependency assignments
func ValidateParentDependencyConflicts(parentDependencyMap map[string][]ParentDependencyInfo, logger *zap.Logger) error {
	for moduleName, parentDeps := range parentDependencyMap {
		if len(parentDeps) <= 1 {
			continue
		}

		// Check for conflicts: multiple parent dependencies defining the same parameter
		parameterToParents := make(map[string][]string)
		for _, parentDep := range parentDeps {
			for paramName := range parentDep.Parameters {
				parameterToParents[paramName] = append(parameterToParents[paramName], parentDep.EntryID)
			}
		}

		// Report conflicts
		for paramName, parentIDs := range parameterToParents {
			if len(parentIDs) > 1 {
				logger.Error("conflict detected: multiple parent dependencies define the same parameter",
					zap.String("module_name", moduleName),
					zap.String("parameter_name", paramName),
					zap.Strings("conflicting_parent_ids", parentIDs))
				return fmt.Errorf("conflict: multiple parent dependencies for module %s define parameter %s: %v",
					moduleName, paramName, parentIDs)
			}
		}
	}

	return nil
}
