package validate

import "errors"

/*
Require validates that all required dependencies are non-nil.
Pass a map of name → value; if any value is nil, returns an error
with a clear message (e.g. "pool is required").
Use in constructors after options are applied so callers fail fast
instead of scattering nil checks throughout Run() and other methods.
*/
func Require(objs map[string]any) error {
	for name, obj := range objs {
		if obj == nil {
			return errors.New(name + " is required")
		}
	}
	return nil
}
