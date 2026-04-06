package adaptive

/*
Ratio expresses one signal relative to another. This
is the fundamental way to combine two measurements
without introducing a constant. Instead of scaling a
signal by some magic multiplier, you divide it by
another observed signal. The result is dimensionless
and self-scaling.

Ratio expects exactly two values per call: the
numerator and the denominator. When the denominator
is zero, the ratio is zero — there is no signal to
be relative to.
*/
type Ratio struct {
	raw      float64
	smoother *EMA
}

/*
NewRatio creates a new Ratio. The output is smoothed
by an EMA that bootstraps itself from the observed
ratios.
*/
func NewRatio(raw float64) *Ratio {
	return &Ratio{raw: raw, smoother: NewEMA()}
}

/*
Next accepts two values — numerator and denominator —
and returns the smoothed ratio. The denominator must
be the second value.
*/
func (ratio *Ratio) Next(
	out float64, values ...float64,
) (result float64, err error) {
	for _, observation := range values {
		if observation == 0 {
			continue
		}

		ratio.raw = ratio.raw / observation
	}

	return ratio.smoother.Next(out, ratio.raw)
}

/*
Reset clears the Ratio back to its initial state.
*/
func (ratio *Ratio) Reset() error {
	ratio.raw = 0
	return ratio.smoother.Reset()
}
