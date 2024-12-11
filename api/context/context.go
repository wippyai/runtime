// Package context is used to pass context between different parts of the application and not allocate
package context

type Key struct {
	name string
}

func (ck *Key) String() string {
	return ck.name
}

var (
	LoggerKey      = &Key{name: "logger"}      //nolint:gochecknoglobals
	CfgFilenameKey = &Key{name: "cfgfilename"} //nolint:gochecknoglobals
)
