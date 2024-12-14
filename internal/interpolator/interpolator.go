package interpolator

// Interpolator handles variable and file interpolation within payloads.
type Interpolator struct {
	sr        *replacer // replacer instance
	replacers []Replacer
}

// Replacer represents a function to handle replacement based on a protocol.
type Replacer func(srt string, ctx interface{}) (string, error)

// NewInterpolator creates a new Interpolator.
func NewInterpolator(replacers ...Replacer) *Interpolator {
	i := &Interpolator{replacers: replacers}
	i.sr = newStringReplacer(replaceString)

	return i
}

// Interpolate performs variable and file interpolation on the payload data.
func (i *Interpolator) Interpolate(in interface{}, ctx interface{}) (interface{}, error) {
	return i.sr.Replace(in, replaceContext{
		replacers: i.replacers,
		ctx:       ctx,
	})
}

type replaceContext struct {
	replacers []Replacer
	ctx       interface{}
}

func replaceString(s string, ctx interface{}) (string, error) {
	if replaceCtx, ok := ctx.(replaceContext); ok {
		var err error
		for _, replaceFunc := range replaceCtx.replacers {
			s, err = replaceFunc(s, replaceCtx.ctx)
			if err != nil {
				return s, err
			}
		}
	}

	return s, nil
}
