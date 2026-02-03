package cache

// CompileKey returns the storage key for a compile fingerprint.
func CompileKey(fingerprint string) string {
	return HashStrings("compile", fingerprint)
}

// TypecheckKey returns the storage key for a typecheck fingerprint.
func TypecheckKey(fingerprint string) string {
	return HashStrings("typecheck", fingerprint)
}
