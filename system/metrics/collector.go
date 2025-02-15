package metrics

// baseCollector provides common functionality for all metric types
type baseCollector struct {
	name   string
	help   string
	mType  string
	labels []string
}

func newBaseCollector(name, help, mType string, labels []string) baseCollector {
	return baseCollector{
		name:   name,
		help:   help,
		mType:  mType,
		labels: labels,
	}
}

func (c *baseCollector) Name() string {
	return c.name
}

func (c *baseCollector) Help() string {
	return c.help
}

func (c *baseCollector) Type() string {
	return c.mType
}

func (c *baseCollector) Labels() []string {
	return c.labels
}
