package adaptive

type Spread struct {
	smoother *EMA
}

func NewSpread() *Spread {
	return &Spread{smoother: NewEMA()}
}

func (spread *Spread) Next(out float64, values ...float64) (float64, error) {
	for _, observation := range values {
		deviation := observation - out
		return spread.smoother.Next(0, deviation*deviation)
	}

	return out, nil
}

func (spread *Spread) Reset() error {
	return spread.smoother.Reset()
}
