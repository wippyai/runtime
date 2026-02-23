// SPDX-License-Identifier: MPL-2.0

package version

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
	BuiltBy = "unknown"
)

func Short() string {
	return Version
}
