package cli

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Parse(args []string) error {
	return nil
}
