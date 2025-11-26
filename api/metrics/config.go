package metrics

type Config struct {
	Interceptor struct {
		Enabled bool `mapstructure:"enabled"`
	} `mapstructure:"interceptor"`

	Buffer struct {
		Size int `mapstructure:"size"`
	} `mapstructure:"buffer"`
}
