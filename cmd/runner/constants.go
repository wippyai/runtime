package main

const (
	// Module operation statuses
	StatusFromCache       = "from cache"
	StatusDownloaded      = "downloaded"
	StatusFromReplacement = "from replacement"
	StatusSkipped         = "skipped"
	StatusRemoved         = "removed"

	// Module operation actions
	ActionInstalled = "installed"
	ActionUpdated   = "updated"
	ActionRemoved   = "removed"

	// Default directories
	DefaultModulesDir = ".wippy"
	DefaultSrcDir     = "."

	// Log messages
	LogInstallingDependencies = "Installing dependencies from lock file"
	LogUpdatingDependencies   = "Updating dependencies"
	LogInstallationCompleted  = "Installation completed"
	LogLockFileOperations     = "Lock file operations: %d installs, %d updates, %d removals:"
	LogPackageOperations      = "Package operations: %d installs, %d updates, %d removals:"
)
