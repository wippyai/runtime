package main

import (
	"fmt"

	"github.com/ponyruntime/pony/moduleloader"
)

// ShowResults displays the operation results in a formatted way
// This is the new method that replaces DisplayModuleStatistics
func (dm *DependencyManager) ShowResults(stats *ModuleOperationStats) {
	if !stats.HasOperations() {
		return
	}

	// Display operation summary using the same format as LogPackageOperations
	dm.logger.Info(fmt.Sprintf(LogPackageOperations,
		stats.Installed, stats.Updated, stats.Removed))

	// Display detailed operations from our new methods
	for _, op := range stats.Operations {
		var statusText string
		switch op.Action {
		case ActionInstalled:
			statusText = fmt.Sprintf(" - Installing %s: %s", op.Name, op.Version)
		case ActionUpdated:
			statusText = fmt.Sprintf(" - Updating %s: %s → %s", op.Name, op.OldVersion, op.Version)
		case ActionRemoved:
			statusText = fmt.Sprintf(" - Removing %s: %s", op.Name, op.Version)
		case ActionSkipped:
			// Only show Skipping messages in verbose mode
			if stats.Verbose {
				statusText = fmt.Sprintf(" - Skipping %s: %s", op.Name, op.Version)
			} else {
				continue
			}
		default:
			continue
		}
		dm.logger.Info(statusText)
	}

	// Display module operations from moduleloader
	for _, stat := range stats.ModuleStats {
		dm.displayModuleOperation(stat, stats.Verbose)
	}

	// Removed modules are now displayed through Operations above
}

// displayModuleOperation displays a single module operation with appropriate status message
func (dm *DependencyManager) displayModuleOperation(stat moduleloader.ModuleStats, verbose bool) {
	var statusText string

	switch stat.Status {
	case StatusFromCache:
		// Only show Skipping messages in verbose mode
		if verbose {
			statusText = fmt.Sprintf(" - Skipping %s: %s", stat.Name, stat.Version)
		} else {
			return
		}
	case StatusDownloaded:
		statusText = fmt.Sprintf(" - Downloading %s: %s", stat.Name, stat.Version)
	case StatusFromReplacement:
		statusText = fmt.Sprintf(" - Using %s: %s (from replacement)", stat.Name, stat.Version)
	case StatusSkipped:
		// Only show Skipping messages in verbose mode
		if verbose {
			statusText = fmt.Sprintf(" - Skipping %s: %s", stat.Name, stat.Version)
		} else {
			return
		}
	default:
		// Skip unknown statuses (including StatusRemoved to avoid duplication)
		return
	}

	dm.logger.Info(statusText)
}
