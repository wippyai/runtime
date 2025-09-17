package deps

// ModuleOperationStats holds comprehensive statistics for module operations
// This structure is created at the beginning of update/install commands
// and passed by pointer to different functions to accumulate data
type ModuleOperationStats struct {
	// Operation counts by type
	Installed int // Number of newly installed modules
	Updated   int // Number of updated modules
	Removed   int // Number of removed modules
	Skipped   int // Number of skipped modules

	// Detailed operation records
	Operations []ModuleOperation // Individual operation details

	// Module statistics from moduleloader
	ModuleStats []ModuleStats // Status information from moduleloader

	// Removed module names (for display purposes)
	RemovedModuleNames []string // Names of removed modules for logging

	// Verbose flag to control output detail
	Verbose bool // Whether to show detailed output including skipped modules
}

// NewModuleOperationStats creates a new empty statistics structure
func NewModuleOperationStats(verbose bool) *ModuleOperationStats {
	return &ModuleOperationStats{
		Operations:         make([]ModuleOperation, 0),
		ModuleStats:        make([]ModuleStats, 0),
		RemovedModuleNames: make([]string, 0),
		Verbose:            verbose,
	}
}

// AddOperation adds a new operation to the statistics and updates counters
func (s *ModuleOperationStats) AddOperation(operation ModuleOperation) {
	s.Operations = append(s.Operations, operation)

	switch operation.Action {
	case ActionInstalled:
		s.Installed++
	case ActionUpdated:
		s.Updated++
	case ActionRemoved:
		s.Removed++
	case ActionSkipped:
		s.Skipped++
	}
}

// AddModuleStats adds module statistics from moduleloader
func (s *ModuleOperationStats) AddModuleStats(stats []ModuleStats) {
	s.ModuleStats = append(s.ModuleStats, stats...)
}

// AddRemovedModule adds a removed module name and increments removal counter
func (s *ModuleOperationStats) AddRemovedModule(moduleName string) {
	s.RemovedModuleNames = append(s.RemovedModuleNames, moduleName)
	s.Removed++
}

// GetTotalOperations returns the total number of operations across all types
func (s *ModuleOperationStats) GetTotalOperations() int {
	return s.Installed + s.Updated + s.Removed + s.Skipped
}

// HasOperations returns true if there are any operations or module stats recorded
func (s *ModuleOperationStats) HasOperations() bool {
	return s.GetTotalOperations() > 0 || len(s.ModuleStats) > 0
}

// AddInstalled increments the installed counter and adds operation record
func (s *ModuleOperationStats) AddInstalled(moduleName, version string) {
	s.Installed++
	operation := ModuleOperation{
		Name:    moduleName,
		Version: version,
		Action:  ActionInstalled,
	}
	s.Operations = append(s.Operations, operation)
}

// AddUpdated increments the updated counter and adds operation record
func (s *ModuleOperationStats) AddUpdated(moduleName, version, oldVersion string) {
	s.Updated++
	operation := ModuleOperation{
		Name:       moduleName,
		Version:    version,
		OldVersion: oldVersion,
		Action:     ActionUpdated,
	}
	s.Operations = append(s.Operations, operation)
}

// AddRemoved increments the removed counter and adds operation record
func (s *ModuleOperationStats) AddRemoved(moduleName, version string) {
	s.Removed++
	operation := ModuleOperation{
		Name:    moduleName,
		Version: version,
		Action:  ActionRemoved,
	}
	s.Operations = append(s.Operations, operation)
}

// AddSkipped increments the skipped counter and adds operation record
func (s *ModuleOperationStats) AddSkipped(moduleName, version string) {
	s.Skipped++
	operation := ModuleOperation{
		Name:    moduleName,
		Version: version,
		Action:  ActionSkipped,
	}
	s.Operations = append(s.Operations, operation)
}

// Reset clears all statistics and resets all counters to zero
func (s *ModuleOperationStats) Reset() {
	s.Installed = 0
	s.Updated = 0
	s.Removed = 0
	s.Skipped = 0
	s.Operations = s.Operations[:0]
	s.ModuleStats = s.ModuleStats[:0]
	s.RemovedModuleNames = s.RemovedModuleNames[:0]
}
