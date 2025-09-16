package moduleloader

const (
	// Module status constants
	StatusFromCache       = "from cache"
	StatusDownloaded      = "downloaded"
	StatusFromReplacement = "from replacement"
	StatusSkipped         = "skipped"

	// Default directories
	DefaultVendorFolder = ".wippy"
	DefaultModulesDir   = ".wippy"
	DefaultSrcDir       = "."
)
