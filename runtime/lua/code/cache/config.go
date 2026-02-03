package cache

// DefaultDir is the default on-disk cache directory relative to the working dir.
const DefaultDir = ".wippy/cache/lua"

// Mode controls cache read/write behavior.
type Mode string

const (
	ModeOff       Mode = "off"
	ModeReadOnly  Mode = "readonly"
	ModeReadWrite Mode = "readwrite"
)

// Config controls cache behavior.
type Config struct {
	Dir              string
	Mode             Mode
	Enabled          bool
	CompileEnabled   bool
	TypecheckEnabled bool
}

// Normalize applies default values.
func (c Config) Normalize() Config {
	if !c.Enabled {
		c.Mode = ModeOff
	}
	if c.Dir == "" {
		c.Dir = DefaultDir
	}
	if c.Mode == "" {
		c.Mode = ModeReadWrite
	}
	return c
}

// AllowsRead reports whether reads are permitted.
func (c Config) AllowsRead() bool {
	c = c.Normalize()
	return c.Enabled && c.Mode != ModeOff
}

// AllowsWrite reports whether writes are permitted.
func (c Config) AllowsWrite() bool {
	c = c.Normalize()
	return c.Enabled && c.Mode == ModeReadWrite
}

// ParseMode converts a string to a cache mode.
func ParseMode(v string) Mode {
	switch Mode(v) {
	case ModeOff:
		return ModeOff
	case ModeReadOnly:
		return ModeReadOnly
	case ModeReadWrite:
		return ModeReadWrite
	default:
		return ModeReadWrite
	}
}
