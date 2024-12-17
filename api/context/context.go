// Package context is used to pass context between different parts of the application and not allocate
package context

type Key string

const (
	LoggerKey      = Key("logger")      //nolint:gochecknoglobals
	CfgFilenameKey = Key("cfgfilename") //nolint:gochecknoglobals
)
