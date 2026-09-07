// SPDX-License-Identifier: MPL-2.0

package cdc

import "context"

type Engine interface {
	SourceInspector
	SourceStreamer
}

type composite struct {
	engines []Engine
}

func NewComposite(engines ...Engine) *composite {
	return &composite{engines: engines}
}

func (c *composite) List() []SourceInfo {
	out := make([]SourceInfo, 0)
	for _, e := range c.engines {
		out = append(out, e.List()...)
	}
	return out
}

func (c *composite) Get(name string) (SourceInfo, bool) {
	for _, e := range c.engines {
		if info, ok := e.Get(name); ok {
			return info, true
		}
	}
	return SourceInfo{}, false
}

func (c *composite) Stream(ctx context.Context, name string, opts StreamOptions) (ChangeStream, SourceInfo, error) {
	for _, e := range c.engines {
		if _, ok := e.Get(name); ok {
			return e.Stream(ctx, name, opts)
		}
	}
	return nil, SourceInfo{}, ErrSourceNotFound
}
