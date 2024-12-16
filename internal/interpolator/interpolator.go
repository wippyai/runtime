package interpolator

// Interpolator performs variable and file interpolation within payloads.
type Interpolator struct {
	sr        *replacer
	replacers []Replacer
}

// Replacer is a function that transforms a string.
type Replacer func(srt string, ctx interface{}) (string, error)

// NewInterpolator creates a new Interpolator with the provided Replacers.
// It traverses the input data structure (maps, slices, structs, etc.) recursively,
// applying the Replacer functions to all strings found within it.
func NewInterpolator(replacers ...Replacer) *Interpolator {
	i := &Interpolator{replacers: replacers}
	i.sr = newStringReplacer(replaceString)
	return i
}

// Interpolate performs interpolation on the input data using the registered Replacers.
func (i *Interpolator) Interpolate(in interface{}, ctx interface{}) (interface{}, error) {
	// the replacer is the one responsible to iterate over the data structure
	// recursively.
	return i.sr.Replace(in, replaceContext{
		replacers: i.replacers,
		ctx:       ctx,
	})
}

// replaceContext holds the Replacers and context for a single replacement operation.
// It is used by the internal replacer.
type replaceContext struct {
	replacers []Replacer  // List of replacer functions.
	ctx       interface{} // Context passed to the replacers.
}

// replaceString applies a chain of Replacer functions to a single string.
// It iterates through each registered Replacer and applies it sequentially.
func replaceString(s string, ctx interface{}) (string, error) {
	if replaceCtx, ok := ctx.(replaceContext); ok {
		var err error
		// execute the chain of replacers
		for _, replaceFunc := range replaceCtx.replacers {
			s, err = replaceFunc(s, replaceCtx.ctx)
			if err != nil {
				return s, err
			}
		}
	}
	return s, nil
}
