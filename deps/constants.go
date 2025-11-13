package deps

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
	ActionSkipped   = "skipped"

	// Default directories
	DefaultModulesDir  = ".wippy"
	DefaultSrcDir      = "."
	TempUpdateDir      = ".wippy-update.tmp"
	TempVendorFolder   = ".wippy-update.tmp/vendor"
	VendorSubdirectory = "vendor"

	// Log messages
	LogInstallingDependencies = "Installing dependencies from lock file"
	LogUpdatingDependencies   = "Updating dependencies"
	LogInstallationCompleted  = "Installation completed"
	LogUpdatingCompleted      = "Updating completed"
	LogLockFileOperations     = "Lock file operations: %d installs, %d updates, %d removals:"
	LogPackageOperations      = "Package operations: %d installs, %d updates, %d removals:"
)
