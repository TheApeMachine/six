package automata

/*
Wavefront tracks which cells in the CA lattice are active, manages
per-cell energy decay, and detects global convergence by monitoring
the rolling Hamming distance between consecutive ticks.
*/
type Wavefront struct {
	activeCells map[string]bool
	energy      map[string]int
	decayRate   int
	epsilon     int
	windowSize  int
	history     []int
}

/*
wavefrontOpts configures a Wavefront with options.
*/
type wavefrontOpts func(*Wavefront)

/*
NewWavefront instantiates an active wavefront tracker with energy decay
and convergence detection. Default decay rate is 8 ticks, epsilon is 1
(convergence requires zero Hamming delta), window size is 3 consecutive
ticks.
*/
func NewWavefront(opts ...wavefrontOpts) *Wavefront {
	wavefront := &Wavefront{
		activeCells: make(map[string]bool),
		energy:      make(map[string]int),
		decayRate:   8,
		epsilon:     1,
		windowSize:  3,
	}

	for _, opt := range opts {
		opt(wavefront)
	}

	return wavefront
}

/*
Activate marks a cell as active and resets its energy to the full decay
budget. Cells that were previously sleeping are re-awakened.
*/
func (wavefront *Wavefront) Activate(key []byte) {
	mapKey := string(key)
	wavefront.activeCells[mapKey] = true
	wavefront.energy[mapKey] = wavefront.decayRate
}

/*
Tick advances one CA step. Decrements energy for every active cell and
puts cells with depleted energy to sleep. Returns the keys of cells
that remain active and should be processed this tick.
*/
func (wavefront *Wavefront) Tick() [][]byte {
	var active [][]byte

	for key, isActive := range wavefront.activeCells {
		if !isActive {
			continue
		}

		wavefront.energy[key]--

		if wavefront.energy[key] <= 0 {
			wavefront.activeCells[key] = false
			continue
		}

		active = append(active, []byte(key))
	}

	return active
}

/*
RecordDelta appends a Hamming distance observation to the rolling window.
Entries beyond windowSize are discarded from the front.
*/
func (wavefront *Wavefront) RecordDelta(delta int) {
	wavefront.history = append(wavefront.history, delta)

	if len(wavefront.history) > wavefront.windowSize {
		wavefront.history = wavefront.history[len(wavefront.history)-wavefront.windowSize:]
	}
}

/*
Converged returns true when every Hamming delta in the rolling window
falls strictly below epsilon for windowSize consecutive ticks.
*/
func (wavefront *Wavefront) Converged() bool {
	if len(wavefront.history) < wavefront.windowSize {
		return false
	}

	for _, delta := range wavefront.history {
		if delta >= wavefront.epsilon {
			return false
		}
	}

	return true
}

/*
ActiveCount returns the number of currently awake cells.
*/
func (wavefront *Wavefront) ActiveCount() int {
	count := 0

	for _, isActive := range wavefront.activeCells {
		if isActive {
			count++
		}
	}

	return count
}

/*
WavefrontWithDecayRate sets the number of ticks a cell stays active
after its last state change before going to sleep.
*/
func WavefrontWithDecayRate(rate int) wavefrontOpts {
	return func(wavefront *Wavefront) {
		wavefront.decayRate = rate
	}
}

/*
WavefrontWithEpsilon sets the convergence threshold. Convergence is
detected when every delta in the rolling window falls strictly below
this value.
*/
func WavefrontWithEpsilon(epsilon int) wavefrontOpts {
	return func(wavefront *Wavefront) {
		wavefront.epsilon = epsilon
	}
}

/*
WavefrontWithWindowSize sets how many consecutive ticks must satisfy
the epsilon condition before convergence is declared.
*/
func WavefrontWithWindowSize(size int) wavefrontOpts {
	return func(wavefront *Wavefront) {
		wavefront.windowSize = size
	}
}
