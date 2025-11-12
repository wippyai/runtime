package version

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
	BuiltBy = "unknown"
)

func Info() string {
	return fmt.Sprintf("wippy %s\ncommit: %s\nbuilt: %s\nby: %s",
		Version, Commit, Date, BuiltBy)
}

func Short() string {
	return Version
}
